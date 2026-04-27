package runtime

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

func TestCommandPipelinePolicyStoreAndTraceContracts(t *testing.T) {
	ctx := context.Background()
	policy := &recordingPolicy{}
	monitor := &recordingMonitor{}
	rt := NewRuntime(Config{
		PolicyEngine: policy,
		Pipeline: PipelineComponents{
			IntentAnalyzer: staticIntentAnalyzer{},
			Planner:        staticPlanner{},
			Validator:      staticValidator{},
			Router:         staticRouter{},
			Dispatcher:     staticDispatcher{},
			TaskMonitor:    monitor,
		},
	})

	result := mustExecuteCommand(t, ctx, rt, StartRunCommand{RunID: "run-command", RootTaskID: "root", Request: "ship v1"})
	if got := result.([]any)[0].(Run); got.Status != RunStatusCreated {
		t.Fatalf("StartRun command should preserve created state, got %#v", got)
	}
	if _, err := rt.Begin(ctx); err != nil {
		t.Fatalf("Begin() error = %v", err)
	}

	advanced := mustAdvanceRun(t, ctx, rt, "run-command")
	assertPipelineOutcome(t, ctx, rt, policy, monitor, advanced)
}

func TestExecuteCommandPersistsThroughUnitOfWork(t *testing.T) {
	ctx := context.Background()
	durable := &recordingUnitOfWork{store: NewMemoryRuntime()}
	rt := NewRuntime(Config{StoreProvider: recordingStoreProvider{uow: durable}})

	mustExecuteCommand(t, ctx, rt, StartRunCommand{RunID: "run-store", RootTaskID: "root", Request: "persist me"})
	if !durable.committed {
		t.Fatalf("ExecuteCommand did not commit UnitOfWork")
	}
	if _, err := durable.store.Run(ctx, "run-store"); err != nil {
		t.Fatalf("durable RunStore missing run: %v", err)
	}
	if _, err := durable.store.Task(ctx, "run-store", "root"); err != nil {
		t.Fatalf("durable TaskStore missing root task: %v", err)
	}
	if events, err := durable.store.RunEvents(ctx, "run-store"); err != nil || len(events) == 0 {
		t.Fatalf("durable EventStore missing events: events=%#v err=%v", events, err)
	}

	mustExecuteCommand(t, ctx, rt, CreateTaskCommand{RunID: "run-store", TaskID: "worker", OwnerAgentID: "agent-a"})
	if _, err := durable.store.Task(ctx, "run-store", "worker"); err != nil {
		t.Fatalf("durable TaskStore missing command-created task: %v", err)
	}
}

func TestPolicyAppliesToRuntimeBoundaries(t *testing.T) {
	ctx := context.Background()
	policy := &recordingPolicy{deny: map[PolicyOperation]bool{
		PolicyOperationBlackboardWrite: true,
		PolicyOperationToolCall:        true,
		PolicyOperationHandoff:         true,
		PolicyOperationResponsePublish: true,
	}}
	rt := NewRuntime(Config{PolicyEngine: policy})
	run, _, err := rt.StartRun(ctx, StartRunCommand{RunID: "run-policy", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := rt.CreateTask(ctx, CreateTaskCommand{RunID: run.ID, TaskID: "worker", OwnerAgentID: "agent-a"})
	if err != nil {
		t.Fatalf("CreateTask(worker) error = %v", err)
	}
	if err := rt.WriteItem(ctx, BlackboardItem{RunID: run.ID, TaskID: task.ID, Source: SourceIdentity{Type: SourceAgent, ID: "agent-a"}}); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("blackboard write should be policy denied, got %v", err)
	}
	rt.RegisterTool(Tool{Name: "readonly", EffectType: ToolEffectReadOnly})
	if _, err := rt.InvokeTool(ctx, ToolInvocation{RunID: run.ID, TaskID: task.ID, ToolName: "readonly"}); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("tool call should be policy denied, got %v", err)
	}
	if err := rt.RequestHandoff(ctx, HandoffCommand{RunID: run.ID, TaskID: task.ID, FromAgentID: "agent-a", ToAgentID: "agent-b", TaskVersion: task.Version}); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("handoff should be policy denied, got %v", err)
	}

	policy.deny = map[PolicyOperation]bool{PolicyOperationResponsePublish: true}
	response, err := rt.CreateTask(ctx, CreateTaskCommand{RunID: run.ID, TaskID: "response", Type: TaskTypeResponse, OwnerComponent: "response_composer"})
	if err != nil {
		t.Fatalf("CreateTask(response) error = %v", err)
	}
	lease := leaseTask(t, ctx, rt, run.ID, response.ID, HolderComponent, "response_composer")
	if err := rt.SubmitResponseOutput(ctx, SubmitResponseOutputCommand{
		RunID:       run.ID,
		TaskID:      response.ID,
		LeaseID:     lease.ID,
		HolderType:  HolderComponent,
		HolderID:    "response_composer",
		TaskVersion: response.Version,
		Payload:     "safe answer",
	}); err != nil {
		t.Fatalf("SubmitResponseOutput() error = %v", err)
	}
	message := rt.ResponseOutbox(run.ID)[0]
	if err := rt.PublishResponse(ctx, PublishResponseCommand{RunID: run.ID, MessageID: message.ID}); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("response publish should be policy denied, got %v", err)
	}
	after := rt.ResponseOutbox(run.ID)[0]
	if after.Status != UserMessageQueued {
		t.Fatalf("denied publish must not mutate message status, got %#v", after)
	}
}

func TestPolicyRequireApprovalAndPauseEffectsBlockSensitiveOperations(t *testing.T) {
	ctx := context.Background()

	dispatchPolicy := &recordingPolicy{effects: map[PolicyOperation]PolicyEffect{PolicyOperationDispatch: PolicyEffectRequireApproval}}
	dispatchRT := NewRuntime(Config{PolicyEngine: dispatchPolicy})
	dispatchRun := mustStartRun(t, ctx, dispatchRT, "run-policy-dispatch")
	dispatchTask := mustCreateTask(t, ctx, dispatchRT, CreateTaskCommand{RunID: dispatchRun.ID, TaskID: "worker", OwnerAgentID: "agent-a"})
	if _, err := dispatchRT.DispatchTask(ctx, DispatchTaskCommand{RunID: dispatchRun.ID, TaskID: dispatchTask.ID, TargetAgentID: "agent-a"}); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("dispatch require_approval should block command, got %v", err)
	}
	assertPolicyBlockedTask(t, ctx, dispatchRT, dispatchRun.ID, dispatchTask.ID, TaskStatusPaused, RunStatusWaitingApproval)
	if collectEventTypes(dispatchRT.Events(dispatchRun.ID)).Contains(EventTaskDispatched) {
		t.Fatalf("dispatch require_approval must not queue a task envelope")
	}

	handoffPolicy := &recordingPolicy{effects: map[PolicyOperation]PolicyEffect{PolicyOperationHandoff: PolicyEffectRequireApproval}}
	handoffRT := NewRuntime(Config{PolicyEngine: handoffPolicy})
	handoffRun := mustStartRun(t, ctx, handoffRT, "run-policy-handoff")
	handoffTask := mustCreateTask(t, ctx, handoffRT, CreateTaskCommand{RunID: handoffRun.ID, TaskID: "handoff", OwnerAgentID: "agent-a"})
	if err := handoffRT.RequestHandoff(ctx, HandoffCommand{RunID: handoffRun.ID, TaskID: handoffTask.ID, FromAgentID: "agent-a", ToAgentID: "agent-b", TaskVersion: handoffTask.Version}); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("handoff require_approval should block command, got %v", err)
	}
	assertPolicyBlockedTask(t, ctx, handoffRT, handoffRun.ID, handoffTask.ID, TaskStatusPaused, RunStatusWaitingApproval)
	afterHandoff := mustLoadTask(t, ctx, handoffRT, handoffRun.ID, handoffTask.ID)
	if afterHandoff.OwnerAgentID != "agent-a" {
		t.Fatalf("blocked handoff changed owner: %#v", afterHandoff)
	}

	toolPolicy := &recordingPolicy{effects: map[PolicyOperation]PolicyEffect{PolicyOperationToolCall: PolicyEffectPause}}
	toolRT := NewRuntime(Config{PolicyEngine: toolPolicy})
	toolRT.RegisterTool(Tool{Name: "deploy", EffectType: ToolEffectWrite})
	toolRun := mustStartRun(t, ctx, toolRT, "run-policy-tool")
	toolTask := mustCreateTask(t, ctx, toolRT, CreateTaskCommand{RunID: toolRun.ID, TaskID: "tool", Type: TaskTypeAction, OwnerAgentID: "agent-a"})
	toolLease := leaseTask(t, ctx, toolRT, toolRun.ID, toolTask.ID, HolderAgent, "agent-a")
	if _, err := toolRT.InvokeTool(ctx, ToolInvocation{RunID: toolRun.ID, TaskID: toolTask.ID, LeaseID: toolLease.ID, HolderType: HolderAgent, HolderID: "agent-a", TaskVersion: toolTask.Version, ToolName: "deploy"}); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("tool_call pause should block command, got %v", err)
	}
	assertPolicyBlockedTask(t, ctx, toolRT, toolRun.ID, toolTask.ID, TaskStatusPaused, RunStatusBlocked)
	if active := toolRT.ActiveLeaseCount(toolRun.ID, toolTask.ID); active != 0 {
		t.Fatalf("policy pause must release active lease, got %d", active)
	}

	actionPolicy := &recordingPolicy{effects: map[PolicyOperation]PolicyEffect{PolicyOperationAction: PolicyEffectPause}}
	actionRT := NewRuntime(Config{PolicyEngine: actionPolicy})
	actionRun := mustStartRun(t, ctx, actionRT, "run-policy-action")
	actionTask := mustCreateTask(t, ctx, actionRT, CreateTaskCommand{RunID: actionRun.ID, TaskID: "action", Type: TaskTypeAction, OwnerAgentID: "agent-a"})
	actionLease := leaseTask(t, ctx, actionRT, actionRun.ID, actionTask.ID, HolderAgent, "agent-a")
	if _, err := actionRT.StartActionAttempt(ctx, StartActionAttemptCommand{RunID: actionRun.ID, TaskID: actionTask.ID, LeaseID: actionLease.ID, HolderType: HolderAgent, HolderID: "agent-a", TaskVersion: actionTask.Version, ToolName: "deploy"}); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("action pause should block command, got %v", err)
	}
	assertPolicyBlockedTask(t, ctx, actionRT, actionRun.ID, actionTask.ID, TaskStatusPaused, RunStatusBlocked)
	if active := actionRT.ActiveLeaseCount(actionRun.ID, actionTask.ID); active != 0 {
		t.Fatalf("policy pause must release action lease, got %d", active)
	}

	responsePolicy := &recordingPolicy{effects: map[PolicyOperation]PolicyEffect{PolicyOperationResponsePublish: PolicyEffectRequireApproval}}
	responseRT := NewRuntime(Config{PolicyEngine: responsePolicy})
	responseRun := mustStartRun(t, ctx, responseRT, "run-policy-response")
	responseTask := mustCreateTask(t, ctx, responseRT, CreateTaskCommand{RunID: responseRun.ID, TaskID: "response", Type: TaskTypeResponse, OwnerComponent: "response_composer"})
	responseLease := leaseTask(t, ctx, responseRT, responseRun.ID, responseTask.ID, HolderComponent, "response_composer")
	if err := responseRT.SubmitResponseOutput(ctx, SubmitResponseOutputCommand{RunID: responseRun.ID, TaskID: responseTask.ID, LeaseID: responseLease.ID, HolderType: HolderComponent, HolderID: "response_composer", TaskVersion: responseTask.Version, Payload: "safe"}); err != nil {
		t.Fatalf("SubmitResponseOutput() error = %v", err)
	}
	message := responseRT.ResponseOutbox(responseRun.ID)[0]
	if err := responseRT.PublishResponse(ctx, PublishResponseCommand{RunID: responseRun.ID, MessageID: message.ID}); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("response_publish require_approval should block command, got %v", err)
	}
	responseRunAfter, err := responseRT.Run(ctx, responseRun.ID)
	if err != nil {
		t.Fatalf("Run(response after policy) error = %v", err)
	}
	if responseRunAfter.Status != RunStatusWaitingApproval || !collectEventTypes(responseRT.Events(responseRun.ID)).Contains(EventApprovalRequested) {
		t.Fatalf("response_publish require_approval did not create approval blocker: run=%#v events=%#v", responseRunAfter, responseRT.Events(responseRun.ID))
	}
	if got := responseRT.ResponseOutbox(responseRun.ID)[0].Status; got != UserMessageQueued {
		t.Fatalf("blocked response publish mutated message status to %s", got)
	}
}

func TestMailboxOutboxRetriesBeforeDeadLetter(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(t, ctx, rt, "run-outbox")
	task := mustCreateTask(t, ctx, rt, CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "worker",
		OwnerAgentID: "agent-a",
		RetryPolicy:  RetryPolicy{MaxAttempts: 2, Backoff: time.Nanosecond},
	})
	env := mustDispatchTask(t, ctx, rt, DispatchTaskCommand{RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a"})
	lease, acquired, err := rt.AcquireTaskExecution(ctx, AcquireTaskExecutionCommand{
		RunID:      run.ID,
		TaskID:     task.ID,
		EnvelopeID: env.ID,
		HolderType: HolderAgent,
		HolderID:   "agent-a",
		TTL:        time.Minute,
	})
	if err != nil || !acquired {
		t.Fatalf("AcquireTaskExecution() lease=%#v acquired=%v err=%v", lease, acquired, err)
	}
	if err := rt.DeadLetter(ctx, DeadLetterCommand{EnvelopeID: env.ID, Reason: "transient"}); err != nil {
		t.Fatalf("DeadLetter(retry) error = %v", err)
	}
	retryEnv := mustLoadEnvelope(t, ctx, rt, env.ID)
	if retryEnv.Status != "pending" || retryEnv.NextRetryAt.IsZero() {
		t.Fatalf("expected retry scheduling, got %#v", retryEnv)
	}
	retryTask := mustLoadTask(t, ctx, rt, run.ID, task.ID)
	if retryTask.Status != TaskStatusDispatched || rt.ActiveLeaseCount(run.ID, task.ID) != 0 {
		t.Fatalf("retry should release active lease and redispatch task: task=%#v active=%d", retryTask, rt.ActiveLeaseCount(run.ID, task.ID))
	}
	if _, acquired, err := rt.AcquireTaskExecution(ctx, AcquireTaskExecutionCommand{
		RunID:      run.ID,
		TaskID:     task.ID,
		EnvelopeID: retryEnv.ID,
		HolderType: HolderAgent,
		HolderID:   "agent-a",
		TTL:        time.Minute,
	}); err != nil || !acquired {
		t.Fatalf("retry envelope should be acquirable, acquired=%v err=%v", acquired, err)
	}
	if err := rt.DeadLetter(ctx, DeadLetterCommand{EnvelopeID: env.ID, Reason: "exhausted"}); err != nil {
		t.Fatalf("DeadLetter(dead) error = %v", err)
	}
	blocked := mustLoadTask(t, ctx, rt, run.ID, task.ID)
	if blocked.Status != TaskStatusBlocked {
		t.Fatalf("exhausted mailbox delivery should block task, got %#v", blocked)
	}
}

func TestApprovalResumeTokenRecovery(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(t, ctx, rt, "run-approval")
	approvalTask := mustCreateTask(t, ctx, rt, CreateTaskCommand{RunID: run.ID, TaskID: "approval", OwnerAgentID: "agent-a"})
	approvalLease := leaseTask(t, ctx, rt, run.ID, approvalTask.ID, HolderAgent, "agent-a")
	if err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
		RunID:       run.ID,
		TaskID:      approvalTask.ID,
		LeaseID:     approvalLease.ID,
		HolderType:  HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: approvalTask.Version,
		Report:      TypedReport{Status: ReportStatusNeedsApproval, Summary: "deploy approval"},
	}); err != nil {
		t.Fatalf("SubmitTypedReport(needs_approval) error = %v", err)
	}
	if len(rt.resumeTokens) != 1 {
		t.Fatalf("expected resumable approval blocker, got %#v", rt.resumeTokens)
	}
	if active := rt.ActiveLeaseCount(run.ID, approvalTask.ID); active != 0 {
		t.Fatalf("needs_approval must release active lease, got %d", active)
	}
	for tokenID := range rt.resumeTokens {
		if _, err := rt.RecoverResumeToken(ctx, RecoverResumeTokenCommand{TokenID: tokenID}); err != nil {
			t.Fatalf("RecoverResumeToken() error = %v", err)
		}
	}
}

func TestBlockedReportReleasesLease(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(t, ctx, rt, "run-blocked-report")
	task := mustCreateTask(t, ctx, rt, CreateTaskCommand{RunID: run.ID, TaskID: "worker", OwnerAgentID: "agent-a"})
	lease := leaseTask(t, ctx, rt, run.ID, task.ID, HolderAgent, "agent-a")
	if err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
		RunID:       run.ID,
		TaskID:      task.ID,
		LeaseID:     lease.ID,
		HolderType:  HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: task.Version,
		Report:      TypedReport{Status: ReportStatusBlocked, Summary: "blocked by dependency"},
	}); err != nil {
		t.Fatalf("SubmitTypedReport(blocked) error = %v", err)
	}
	blocked := mustLoadTask(t, ctx, rt, run.ID, task.ID)
	if blocked.Status != TaskStatusBlocked {
		t.Fatalf("blocked report should block task, got %#v", blocked)
	}
	if active := rt.ActiveLeaseCount(run.ID, task.ID); active != 0 {
		t.Fatalf("blocked report must release active lease, got %d", active)
	}
}

func TestActionAttemptReconcileAndSourceIdentitySelector(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(t, ctx, rt, "run-action-selector")
	actionTask := mustCreateTask(t, ctx, rt, CreateTaskCommand{RunID: run.ID, TaskID: "action", Type: TaskTypeAction, OwnerAgentID: "agent-a"})
	actionLease := leaseTask(t, ctx, rt, run.ID, actionTask.ID, HolderAgent, "agent-a")
	attempt, err := rt.StartActionAttempt(ctx, StartActionAttemptCommand{
		RunID:       run.ID,
		TaskID:      actionTask.ID,
		LeaseID:     actionLease.ID,
		HolderType:  HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: actionTask.Version,
		ToolName:    "deploy",
	})
	if err != nil {
		t.Fatalf("StartActionAttempt() error = %v", err)
	}
	if _, err := rt.CompleteActionAttempt(ctx, CompleteActionAttemptCommand{RunID: run.ID, TaskID: actionTask.ID, AttemptID: attempt.AttemptID, Status: ActionAttemptUnknown}); err != nil {
		if !errors.Is(err, ErrLeaseNotActive) {
			t.Fatalf("CompleteActionAttempt without lease error = %v, want ErrLeaseNotActive", err)
		}
	}
	if _, err := rt.CompleteActionAttempt(ctx, CompleteActionAttemptCommand{
		RunID:       run.ID,
		TaskID:      actionTask.ID,
		LeaseID:     actionLease.ID,
		HolderType:  HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: actionTask.Version,
		AttemptID:   attempt.AttemptID,
		Status:      ActionAttemptUnknown,
	}); err != nil {
		t.Fatalf("CompleteActionAttempt(unknown with lease) error = %v", err)
	}
	reconcile := mustLoadTask(t, ctx, rt, run.ID, actionTask.ID)
	if reconcile.Status != TaskStatusReconcileRequired {
		t.Fatalf("unknown action attempt should require reconcile, got %#v", reconcile)
	}
	if active := rt.ActiveLeaseCount(run.ID, actionTask.ID); active != 0 {
		t.Fatalf("reconcile-required action attempt must release active lease, got %d", active)
	}

	if err := rt.WriteItem(ctx, BlackboardItem{RunID: run.ID, TaskID: "source", Type: BlackboardItemClaim, Source: SourceIdentity{Type: SourceAgent, ID: "agent-source"}, Visibility: BlackboardVisibilityAgentVisible, Payload: "claim"}); err != nil {
		t.Fatalf("WriteItem() error = %v", err)
	}
	selected, err := rt.SelectItems(ctx, run.ID, BlackboardSelector{SourceTypes: []SourceType{SourceAgent}, SourceIDs: []string{"agent-source"}})
	if err != nil {
		t.Fatalf("SelectItems() error = %v", err)
	}
	if len(selected) != 1 || selected[0].Payload != "claim" {
		t.Fatalf("SourceIdentity selector failed, got %#v", selected)
	}
}

func TestPublishResponseDoesNotHoldRuntimeLockDuringGatewayCall(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(t, ctx, rt, "run-gateway")
	response := mustCreateTask(t, ctx, rt, CreateTaskCommand{RunID: run.ID, TaskID: "response", Type: TaskTypeResponse, OwnerComponent: "response_composer"})
	lease := leaseTask(t, ctx, rt, run.ID, response.ID, HolderComponent, "response_composer")
	if err := rt.SubmitResponseOutput(ctx, SubmitResponseOutputCommand{
		RunID:       run.ID,
		TaskID:      response.ID,
		LeaseID:     lease.ID,
		HolderType:  HolderComponent,
		HolderID:    "response_composer",
		TaskVersion: response.Version,
		Payload:     "hello",
	}); err != nil {
		t.Fatalf("SubmitResponseOutput() error = %v", err)
	}
	rt.SetOutputGateway(reentrantGateway{rt: rt})
	done := make(chan error, 1)
	message := rt.ResponseOutbox(run.ID)[0]
	go func() {
		done <- rt.PublishResponse(ctx, PublishResponseCommand{RunID: run.ID, MessageID: message.ID})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("PublishResponse() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("PublishResponse held runtime lock while gateway re-entered runtime")
	}
}

type recordingPolicy struct {
	operations []PolicyOperation
	deny       map[PolicyOperation]bool
	effects    map[PolicyOperation]PolicyEffect
}

func (p *recordingPolicy) Authorize(_ context.Context, request PolicyRequest) (PolicyDecision, error) {
	p.operations = append(p.operations, request.Operation)
	if p.deny[request.Operation] {
		return PolicyDecision{Effect: PolicyEffectDeny, Reason: "test deny"}, nil
	}
	if effect := p.effects[request.Operation]; effect != "" {
		return PolicyDecision{Effect: effect, Reason: "test " + string(effect)}, nil
	}
	return PolicyDecision{Effect: PolicyEffectAllow}, nil
}

type recordingStoreProvider struct {
	uow *recordingUnitOfWork
}

func (p recordingStoreProvider) Begin(context.Context) (UnitOfWork, error) {
	p.uow.begun = true
	p.uow.committed = false
	p.uow.rolledBack = false
	return p.uow, nil
}

type recordingUnitOfWork struct {
	store      *Runtime
	begun      bool
	committed  bool
	rolledBack bool
}

func (u *recordingUnitOfWork) Runs() RunStore                    { return u.store }
func (u *recordingUnitOfWork) Tasks() TaskStore                  { return u.store }
func (u *recordingUnitOfWork) Events() EventStore                { return u.store }
func (u *recordingUnitOfWork) Blackboard() BlackboardStore       { return u.store }
func (u *recordingUnitOfWork) MailboxOutbox() MailboxOutboxStore { return u.store }
func (u *recordingUnitOfWork) UserMessages() UserMessageStore    { return u.store }
func (u *recordingUnitOfWork) Trace() TraceStore                 { return u.store }
func (u *recordingUnitOfWork) Commit(context.Context) error {
	u.committed = true
	return nil
}
func (u *recordingUnitOfWork) Rollback(context.Context) error {
	u.rolledBack = true
	return nil
}

type reentrantGateway struct {
	rt *Runtime
}

func (g reentrantGateway) Publish(_ context.Context, message UserMessage) error {
	_ = g.rt.ResponseOutbox(message.RunID)
	return nil
}

type staticIntentAnalyzer struct{}

func (staticIntentAnalyzer) AnalyzeIntent(_ context.Context, run Run) (Intent, error) {
	return Intent{RunID: run.ID, Summary: "intent:" + run.Request}, nil
}

type staticPlanner struct{}

func (staticPlanner) CreatePlan(_ context.Context, intent Intent) (TodoPlan, error) {
	return TodoPlan{RunID: intent.RunID, Tasks: []Task{{
		ID:           "planned-worker",
		RunID:        intent.RunID,
		Type:         TaskTypeWorker,
		Goal:         intent.Summary,
		OwnerAgentID: "agent-a",
		Status:       TaskStatusPlanned,
		Version:      1,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}}}, nil
}

type staticValidator struct{}

func (staticValidator) ValidatePlan(context.Context, TodoPlan) error { return nil }

type staticRouter struct{}

func (staticRouter) RouteTasks(_ context.Context, plan TodoPlan) (RoutingPlan, error) {
	return RoutingPlan{RunID: plan.RunID, Routes: []TaskRoute{{TaskID: "planned-worker", TargetAgentID: "agent-a"}}}, nil
}

type staticDispatcher struct{}

func (staticDispatcher) Dispatch(_ context.Context, routing RoutingPlan) ([]TaskEnvelope, error) {
	return []TaskEnvelope{{RunID: routing.RunID, TaskID: "planned-worker", TargetAgentID: "agent-a", Status: "pending"}}, nil
}

type recordingMonitor struct {
	advanced bool
}

func (m *recordingMonitor) Advance(context.Context, Run) error {
	m.advanced = true
	return nil
}

func (m *recordingMonitor) DecideDeadLetter(_ context.Context, env TaskEnvelope, reason string) (TaskMonitorDecision, error) {
	return defaultTaskMonitor{}.DecideDeadLetter(context.Background(), env, reason)
}

func containsSpan(spans []TraceSpan, name string) bool {
	for _, span := range spans {
		if span.Name == name {
			return true
		}
	}
	return false
}

func assertPipelineOutcome(t *testing.T, ctx context.Context, rt *Runtime, policy *recordingPolicy, monitor *recordingMonitor, run Run) {
	t.Helper()
	if run.Status != RunStatusRunning {
		t.Fatalf("expected running after full pipeline, got %#v", run)
	}
	if !monitor.advanced {
		t.Fatalf("TaskMonitor.Advance was not called")
	}
	planned := mustLoadTask(t, ctx, rt, run.ID, "planned-worker")
	if planned.Status != TaskStatusDispatched || planned.OwnerAgentID != "agent-a" {
		t.Fatalf("pipeline did not persist and dispatch planned task: %#v", planned)
	}
	if !slices.Contains(policy.operations, PolicyOperationDispatch) {
		t.Fatalf("dispatch did not go through PolicyEngine, ops=%#v", policy.operations)
	}
	spans := rt.TraceSpans(run.ID)
	if !containsSpan(spans, "runtime.pipeline") || !containsSpan(spans, "policy.authorize.dispatch") || !containsSpan(spans, "mailbox.dispatch") {
		t.Fatalf("expected pipeline/policy/mailbox spans, got %#v", spans)
	}
}

func assertPolicyBlockedTask(t *testing.T, ctx context.Context, rt *Runtime, runID, taskID string, taskStatus TaskStatus, runStatus RunStatus) {
	t.Helper()
	task := mustLoadTask(t, ctx, rt, runID, taskID)
	if task.Status != taskStatus {
		t.Fatalf("policy effect set task status %s, want %s: %#v", task.Status, taskStatus, task)
	}
	run, err := rt.Run(ctx, runID)
	if err != nil {
		t.Fatalf("Run(%s) error = %v", runID, err)
	}
	if run.Status != runStatus {
		t.Fatalf("policy effect set run status %s, want %s: %#v", run.Status, runStatus, run)
	}
	if !collectEventTypes(rt.Events(runID)).Contains(EventTaskPaused) {
		t.Fatalf("policy effect did not emit TaskPaused, events=%#v", rt.Events(runID))
	}
}

func mustExecuteCommand(t *testing.T, ctx context.Context, rt *Runtime, cmd RuntimeCommand) any {
	t.Helper()
	result, err := rt.ExecuteCommand(ctx, cmd)
	if err != nil {
		t.Fatalf("ExecuteCommand(%s) error = %v", cmd.CommandName(), err)
	}
	return result
}

func mustAdvanceRun(t *testing.T, ctx context.Context, rt *Runtime, runID string) Run {
	t.Helper()
	run, err := rt.AdvanceRun(ctx, AdvanceRunCommand{RunID: runID})
	if err != nil {
		t.Fatalf("AdvanceRun() error = %v", err)
	}
	return run
}

func mustStartRun(t *testing.T, ctx context.Context, rt *Runtime, runID string) Run {
	t.Helper()
	run, _, err := rt.StartRun(ctx, StartRunCommand{RunID: runID, RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	return run
}

func mustCreateTask(t *testing.T, ctx context.Context, rt *Runtime, cmd CreateTaskCommand) Task {
	t.Helper()
	task, err := rt.CreateTask(ctx, cmd)
	if err != nil {
		t.Fatalf("CreateTask(%s) error = %v", cmd.TaskID, err)
	}
	return task
}

func mustDispatchTask(t *testing.T, ctx context.Context, rt *Runtime, cmd DispatchTaskCommand) TaskEnvelope {
	t.Helper()
	env, err := rt.DispatchTask(ctx, cmd)
	if err != nil {
		t.Fatalf("DispatchTask(%s) error = %v", cmd.TaskID, err)
	}
	return env
}

func mustLoadEnvelope(t *testing.T, ctx context.Context, rt *Runtime, envelopeID string) TaskEnvelope {
	t.Helper()
	env, err := rt.LoadEnvelope(ctx, envelopeID)
	if err != nil {
		t.Fatalf("LoadEnvelope(%s) error = %v", envelopeID, err)
	}
	return env
}

func mustLoadTask(t *testing.T, ctx context.Context, rt *Runtime, runID, taskID string) Task {
	t.Helper()
	task, err := rt.Task(ctx, runID, taskID)
	if err != nil {
		t.Fatalf("Task(%s) error = %v", taskID, err)
	}
	return task
}
