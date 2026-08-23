package core

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/ports"
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

	result := mustExecuteCommand(ctx, t, rt, StartRunCommand{RunID: "run-command", RootTaskID: "root", Request: "ship v1"})
	if got := result.(StartRunResult).Run; got.Status != RunStatusCreated {
		t.Fatalf("StartRun command should preserve created state, got %#v", got)
	}
	uow, err := rt.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := uow.Rollback(ctx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}

	advanced := mustAdvanceRun(ctx, t, rt, "run-command")
	assertPipelineOutcome(ctx, t, rt, policy, monitor, advanced)
}

func TestStoreFirstPublicReadsUseConfiguredStoreProvider(t *testing.T) {
	ctx := context.Background()
	durable := &recordingUnitOfWork{store: NewMemoryRuntime()}
	rt := NewRuntime(Config{StoreProvider: recordingStoreProvider{uow: durable}})
	run := Run{ID: "run-read", RootTaskID: "root", Status: RunStatusRunning, CreatedAt: time.Now().UTC()}
	task := Task{ID: "task-read", RunID: run.ID, Status: TaskStatusCreated, Version: 1, CreatedAt: time.Now().UTC()}
	if err := durable.store.SaveRun(ctx, run); err != nil {
		t.Fatalf("SaveRun(seed) error = %v", err)
	}
	if err := durable.store.SaveTask(ctx, task); err != nil {
		t.Fatalf("SaveTask(seed) error = %v", err)
	}
	if err := durable.store.AppendEvent(ctx, Event{RunID: run.ID, TaskID: task.ID, Type: EventTaskCreated}); err != nil {
		t.Fatalf("AppendEvent(seed) error = %v", err)
	}
	if err := durable.store.QueueMessage(ctx, UserMessage{ID: "msg-read", RunID: run.ID, TaskID: task.ID, Payload: "queued"}); err != nil {
		t.Fatalf("QueueMessage(seed) error = %v", err)
	}
	if err := durable.store.SaveTraceSpan(ctx, TraceSpan{ID: "span-read", RunID: run.ID, TaskID: task.ID, Name: "seed"}); err != nil {
		t.Fatalf("SaveTraceSpan(seed) error = %v", err)
	}
	if err := durable.store.WriteItem(ctx, BlackboardItem{ID: "bb-read", RunID: run.ID, TaskID: task.ID, Source: SourceIdentity{Type: SourceSystem, ID: "seed"}, Visibility: BlackboardVisibilityAgentVisible, Key: "seed", Payload: "item"}); err != nil {
		t.Fatalf("WriteItem(seed) error = %v", err)
	}
	seedUoW, err := durable.store.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin(seed token) error = %v", err)
	}
	if err := seedUoW.ResumeTokens().SaveResumeToken(ctx, ResumeToken{TokenID: "tok-read", RunID: run.ID, TaskID: task.ID}); err != nil {
		_ = seedUoW.Rollback(ctx)
		t.Fatalf("SaveResumeToken(seed) error = %v", err)
	}
	if err := seedUoW.Commit(ctx); err != nil {
		t.Fatalf("Commit(seed token) error = %v", err)
	}
	if got, err := rt.Run(ctx, run.ID); err != nil || got.ID != run.ID {
		t.Fatalf("Run() read from store = %#v err=%v", got, err)
	}
	if got, err := rt.Task(ctx, run.ID, task.ID); err != nil || got.ID != task.ID {
		t.Fatalf("Task() read from store = %#v err=%v", got, err)
	}
	if events, err := rt.RunEvents(ctx, run.ID); err != nil || len(events) == 0 {
		t.Fatalf("RunEvents() read from store events=%#v err=%v", events, err)
	}
	if ready := mustReadyTasks(context.Background(), t, rt, run.ID); !containsTask(ready, task.ID) {
		t.Fatalf("ReadyTasks() did not read store task: %#v", ready)
	}
	if outbox := mustResponseOutbox(context.Background(), t, rt, run.ID); len(outbox) != 1 || outbox[0].ID != "msg-read" {
		t.Fatalf("ResponseOutbox() read from store = %#v", outbox)
	}
	if spans := rt.TraceSpans(context.Background(), run.ID); !containsSpan(spans, "seed") {
		t.Fatalf("TraceSpans() read from store = %#v", spans)
	}
	if items, err := rt.SelectItems(ctx, run.ID, BlackboardSelector{Keys: []string{"seed"}}); err != nil || len(items) != 1 || items[0].ID != "bb-read" {
		t.Fatalf("SelectItems() read from store items=%#v err=%v", items, err)
	}
	if tokens := mustResumeTokens(ctx, t, rt); tokens["tok-read"].TokenID != "tok-read" {
		t.Fatalf("ResumeTokens() did not read store token: %#v", tokens)
	}
}

func TestDrainResponseOutboxUsesConfiguredStoreProvider(t *testing.T) {
	ctx := context.Background()
	durable := &recordingUnitOfWork{store: NewMemoryRuntime()}
	rt := NewRuntime(Config{StoreProvider: recordingStoreProvider{uow: durable}})
	run := Run{ID: "run-drain", RootTaskID: "root", Status: RunStatusRunning, CreatedAt: time.Now().UTC()}
	if err := durable.store.SaveRun(ctx, run); err != nil {
		t.Fatalf("SaveRun(seed) error = %v", err)
	}
	if err := durable.store.QueueMessage(ctx, UserMessage{ID: "msg-drain", RunID: run.ID, TaskID: "root", Type: UserMessageTypeFinalAnswer, Payload: "ready"}); err != nil {
		t.Fatalf("QueueMessage(seed) error = %v", err)
	}
	published, err := rt.DrainResponseOutbox(ctx)
	if err != nil {
		t.Fatalf("DrainResponseOutbox() error = %v", err)
	}
	if published != 1 {
		t.Fatalf("DrainResponseOutbox() published %d, want 1", published)
	}
	message, err := durable.store.LoadMessage(ctx, run.ID, "msg-drain")
	if err != nil {
		t.Fatalf("LoadMessage() error = %v", err)
	}
	if message.Status != UserMessagePublished {
		t.Fatalf("DrainResponseOutbox() did not publish durable message: %#v", message)
	}
}

func TestExecuteCommandPersistsThroughUnitOfWork(t *testing.T) {
	ctx := context.Background()
	durable := &recordingUnitOfWork{store: NewMemoryRuntime()}
	rt := NewRuntime(Config{StoreProvider: recordingStoreProvider{uow: durable}})

	mustExecuteCommand(ctx, t, rt, StartRunCommand{RunID: "run-store", RootTaskID: "root", Request: "persist me"})
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

	mustExecuteCommand(ctx, t, rt, CreateTaskCommand{RunID: "run-store", TaskID: "worker", OwnerAgentID: "agent-a"})
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
	_ = rt.RegisterTool(Tool{Name: "readonly", EffectType: ToolEffectReadOnly})
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
	lease := leaseTask(ctx, t, rt, run.ID, response.ID, HolderComponent, "response_composer")
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
	message := mustResponseOutbox(context.Background(), t, rt, run.ID)[0]
	if err := rt.PublishResponse(ctx, PublishResponseCommand{RunID: run.ID, MessageID: message.ID}); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("response publish should be policy denied, got %v", err)
	}
	after := mustResponseOutbox(context.Background(), t, rt, run.ID)[0]
	if after.Status != UserMessageQueued {
		t.Fatalf("denied publish must not mutate message status, got %#v", after)
	}
}

func TestPolicyRequireApprovalAndPauseEffectsBlockSensitiveOperations(t *testing.T) {
	ctx := context.Background()

	dispatchPolicy := &recordingPolicy{effects: map[PolicyOperation]PolicyEffect{PolicyOperationDispatch: PolicyEffectRequireApproval}}
	dispatchRT := NewRuntime(Config{PolicyEngine: dispatchPolicy})
	dispatchRun := mustStartRun(ctx, t, dispatchRT, "run-policy-dispatch")
	dispatchTask := mustCreateTask(ctx, t, dispatchRT, CreateTaskCommand{RunID: dispatchRun.ID, TaskID: "worker", OwnerAgentID: "agent-a"})
	if _, err := dispatchRT.DispatchTask(ctx, DispatchTaskCommand{RunID: dispatchRun.ID, TaskID: dispatchTask.ID, TargetAgentID: "agent-a"}); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("dispatch require_approval should block command, got %v", err)
	}
	assertPolicyBlockedTask(ctx, t, dispatchRT, dispatchRun.ID, dispatchTask.ID, TaskStatusPaused, RunStatusWaitingApproval)
	if collectEventTypes(dispatchRT.Events(context.Background(), dispatchRun.ID)).Contains(EventTaskDispatched) {
		t.Fatalf("dispatch require_approval must not queue a task envelope")
	}

	handoffPolicy := &recordingPolicy{effects: map[PolicyOperation]PolicyEffect{PolicyOperationHandoff: PolicyEffectRequireApproval}}
	handoffRT := NewRuntime(Config{PolicyEngine: handoffPolicy})
	handoffRun := mustStartRun(ctx, t, handoffRT, "run-policy-handoff")
	handoffTask := mustCreateTask(ctx, t, handoffRT, CreateTaskCommand{RunID: handoffRun.ID, TaskID: "handoff", OwnerAgentID: "agent-a"})
	if err := handoffRT.RequestHandoff(ctx, HandoffCommand{RunID: handoffRun.ID, TaskID: handoffTask.ID, FromAgentID: "agent-a", ToAgentID: "agent-b", TaskVersion: handoffTask.Version}); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("handoff require_approval should block command, got %v", err)
	}
	assertPolicyBlockedTask(ctx, t, handoffRT, handoffRun.ID, handoffTask.ID, TaskStatusPaused, RunStatusWaitingApproval)
	afterHandoff := mustLoadTask(ctx, t, handoffRT, handoffRun.ID, handoffTask.ID)
	if afterHandoff.OwnerAgentID != "agent-a" {
		t.Fatalf("blocked handoff changed owner: %#v", afterHandoff)
	}

	toolPolicy := &recordingPolicy{effects: map[PolicyOperation]PolicyEffect{PolicyOperationToolCall: PolicyEffectPause}}
	toolRT := NewRuntime(Config{PolicyEngine: toolPolicy})
	toolRun := mustStartRun(ctx, t, toolRT, "run-policy-tool")
	toolTask := mustCreateTask(ctx, t, toolRT, CreateTaskCommand{RunID: toolRun.ID, TaskID: "tool", Type: TaskTypeWorker, AllowsAction: true, OwnerAgentID: "agent-a"})
	toolLease := leaseTask(ctx, t, toolRT, toolRun.ID, toolTask.ID, HolderAgent, "agent-a")
	_ = toolRT.RegisterToolForInvocation(toolRun.ID, toolTask.ID, HolderAgent, "agent-a", Tool{Name: "deploy", EffectType: ToolEffectWrite})
	if _, err := toolRT.InvokeTool(ctx, ToolInvocation{RunID: toolRun.ID, TaskID: toolTask.ID, LeaseID: toolLease.ID, HolderType: HolderAgent, HolderID: "agent-a", TaskVersion: toolTask.Version, ToolName: "deploy"}); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("tool_call pause should block command, got %v", err)
	}
	assertPolicyBlockedTask(ctx, t, toolRT, toolRun.ID, toolTask.ID, TaskStatusPaused, RunStatusBlocked)
	if active := mustActiveLeaseCount(context.Background(), t, toolRT, toolRun.ID, toolTask.ID); active != 0 {
		t.Fatalf("policy pause must release active lease, got %d", active)
	}

	actionPolicy := &recordingPolicy{effects: map[PolicyOperation]PolicyEffect{PolicyOperationAction: PolicyEffectPause}}
	actionRT := NewRuntime(Config{PolicyEngine: actionPolicy})
	actionRun := mustStartRun(ctx, t, actionRT, "run-policy-action")
	actionTask := mustCreateTask(ctx, t, actionRT, CreateTaskCommand{RunID: actionRun.ID, TaskID: "action", Type: TaskTypeWorker, AllowsAction: true, OwnerAgentID: "agent-a"})
	actionLease := leaseTask(ctx, t, actionRT, actionRun.ID, actionTask.ID, HolderAgent, "agent-a")
	if _, err := actionRT.StartActionAttempt(ctx, StartActionAttemptCommand{RunID: actionRun.ID, TaskID: actionTask.ID, LeaseID: actionLease.ID, HolderType: HolderAgent, HolderID: "agent-a", TaskVersion: actionTask.Version, ToolName: "deploy"}); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("action pause should block command, got %v", err)
	}
	assertPolicyBlockedTask(ctx, t, actionRT, actionRun.ID, actionTask.ID, TaskStatusPaused, RunStatusBlocked)
	if active := mustActiveLeaseCount(context.Background(), t, actionRT, actionRun.ID, actionTask.ID); active != 0 {
		t.Fatalf("policy pause must release action lease, got %d", active)
	}

	responsePolicy := &recordingPolicy{effects: map[PolicyOperation]PolicyEffect{PolicyOperationResponsePublish: PolicyEffectRequireApproval}}
	responseRT := NewRuntime(Config{PolicyEngine: responsePolicy})
	responseRun := mustStartRun(ctx, t, responseRT, "run-policy-response")
	responseTask := mustCreateTask(ctx, t, responseRT, CreateTaskCommand{RunID: responseRun.ID, TaskID: "response", Type: TaskTypeResponse, OwnerComponent: "response_composer"})
	responseLease := leaseTask(ctx, t, responseRT, responseRun.ID, responseTask.ID, HolderComponent, "response_composer")
	if err := responseRT.SubmitResponseOutput(ctx, SubmitResponseOutputCommand{RunID: responseRun.ID, TaskID: responseTask.ID, LeaseID: responseLease.ID, HolderType: HolderComponent, HolderID: "response_composer", TaskVersion: responseTask.Version, Payload: "safe"}); err != nil {
		t.Fatalf("SubmitResponseOutput() error = %v", err)
	}
	message := mustResponseOutbox(context.Background(), t, responseRT, responseRun.ID)[0]
	if err := responseRT.PublishResponse(ctx, PublishResponseCommand{RunID: responseRun.ID, MessageID: message.ID}); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("response_publish require_approval should block command, got %v", err)
	}
	responseRunAfter, err := responseRT.Run(ctx, responseRun.ID)
	if err != nil {
		t.Fatalf("Run(response after policy) error = %v", err)
	}
	if responseRunAfter.Status != RunStatusWaitingApproval || !collectEventTypes(responseRT.Events(context.Background(), responseRun.ID)).Contains(EventApprovalRequested) {
		t.Fatalf("response_publish require_approval did not create approval blocker: run=%#v events=%#v", responseRunAfter, responseRT.Events(context.Background(), responseRun.ID))
	}
	if got := mustResponseOutbox(context.Background(), t, responseRT, responseRun.ID)[0].Status; got != UserMessageQueued {
		t.Fatalf("blocked response publish mutated message status to %s", got)
	}
}

func TestMailboxOutboxRetriesBeforeDeadLetter(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(ctx, t, rt, "run-outbox")
	task := mustCreateTask(ctx, t, rt, CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "worker",
		OwnerAgentID: "agent-a",
		RetryPolicy:  RetryPolicy{MaxAttempts: 2, Backoff: time.Nanosecond},
	})
	env := mustDispatchTask(ctx, t, rt, DispatchTaskCommand{RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a"})
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
	retryEnv := mustLoadEnvelope(ctx, t, rt, env.ID)
	if retryEnv.Status != "pending" || retryEnv.NextRetryAt.IsZero() {
		t.Fatalf("expected retry scheduling, got %#v", retryEnv)
	}
	retryTask := mustLoadTask(ctx, t, rt, run.ID, task.ID)
	if retryTask.Status != TaskStatusDispatched || mustActiveLeaseCount(context.Background(), t, rt, run.ID, task.ID) != 0 {
		t.Fatalf("retry should release active lease and redispatch task: task=%#v active=%d", retryTask, mustActiveLeaseCount(context.Background(), t, rt, run.ID, task.ID))
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
	blocked := mustLoadTask(ctx, t, rt, run.ID, task.ID)
	if blocked.Status != TaskStatusBlocked {
		t.Fatalf("exhausted mailbox delivery should block task, got %#v", blocked)
	}
	if active := mustActiveLeaseCount(ctx, t, rt, run.ID, task.ID); active != 0 {
		t.Fatalf("terminal dead-letter should release active lease, got %d", active)
	}
	uow, err := rt.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	entries, err := uow.DeadLetters().ListDeadLetters(ctx, api.DeadLetterSelector{RunID: run.ID, TaskID: task.ID})
	if err != nil {
		t.Fatalf("ListDeadLetters() error = %v", err)
	}
	if err := uow.Rollback(ctx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if len(entries) != 1 || entries[0].EnvelopeID != env.ID || entries[0].Reason != "exhausted" {
		t.Fatalf("dead-letter ledger = %#v, want one entry for %s", entries, env.ID)
	}
}

func TestMailboxOutboxCountsFailureBeforeLeaseAcquisition(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(ctx, t, rt, "run-outbox-pre-acquire")
	task := mustCreateTask(ctx, t, rt, CreateTaskCommand{
		RunID:       run.ID,
		TaskID:      "worker",
		RetryPolicy: RetryPolicy{MaxAttempts: 1},
	})
	env := mustDispatchTask(ctx, t, rt, DispatchTaskCommand{RunID: run.ID, TaskID: task.ID})
	if err := rt.DeadLetter(ctx, DeadLetterCommand{EnvelopeID: env.ID, Reason: "delivery failed"}); err != nil {
		t.Fatalf("DeadLetter() error = %v", err)
	}
	got := mustLoadEnvelope(ctx, t, rt, env.ID)
	if got.Status != "dead" || got.Attempts != 1 {
		t.Fatalf("dead-lettered envelope = %#v, want one recorded attempt", got)
	}
}

func TestApprovalResumeTokenRecovery(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(ctx, t, rt, "run-approval")
	approvalTask := mustCreateTask(ctx, t, rt, CreateTaskCommand{RunID: run.ID, TaskID: "approval", OwnerAgentID: "agent-a"})
	approvalLease := leaseTask(ctx, t, rt, run.ID, approvalTask.ID, HolderAgent, "agent-a")
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
	resumeTokens := mustResumeTokens(ctx, t, rt)
	if len(resumeTokens) != 1 {
		t.Fatalf("expected resumable approval blocker, got %#v", resumeTokens)
	}
	if active := mustActiveLeaseCount(context.Background(), t, rt, run.ID, approvalTask.ID); active != 0 {
		t.Fatalf("needs_approval must release active lease, got %d", active)
	}
	for tokenID := range resumeTokens {
		if _, err := rt.RecoverResumeToken(ctx, RecoverResumeTokenCommand{TokenID: tokenID}); err != nil {
			t.Fatalf("RecoverResumeToken() error = %v", err)
		}
	}
}

func TestDecideApprovalApprovedResumesPausedTaskAndRun(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(ctx, t, rt, "run-approval-decision")
	task := mustCreateTask(ctx, t, rt, CreateTaskCommand{RunID: run.ID, TaskID: "approval", OwnerAgentID: "agent-a"})
	lease := leaseTask(ctx, t, rt, run.ID, task.ID, HolderAgent, "agent-a")

	if err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
		RunID:       run.ID,
		TaskID:      task.ID,
		LeaseID:     lease.ID,
		HolderType:  HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: task.Version,
		Report:      TypedReport{Status: ReportStatusNeedsApproval, Summary: "approve deploy"},
	}); err != nil {
		t.Fatalf("SubmitTypedReport(needs_approval) error = %v", err)
	}
	paused := mustLoadTask(ctx, t, rt, run.ID, task.ID)
	if paused.Status != TaskStatusPaused {
		t.Fatalf("needs_approval should pause task, got %#v", paused)
	}
	waiting, err := rt.Run(ctx, run.ID)
	if err != nil {
		t.Fatalf("Run(waiting approval) error = %v", err)
	}
	if waiting.Status != RunStatusWaitingApproval {
		t.Fatalf("needs_approval should block run, got %#v", waiting)
	}
	tokens := mustResumeTokens(ctx, t, rt)
	if len(tokens) != 1 {
		t.Fatalf("expected one resume token, got %#v", tokens)
	}
	var token ResumeToken
	for _, value := range tokens {
		token = value
	}

	if err := rt.DecideApproval(ctx, DecideApprovalCommand{RunID: "wrong-run", ApprovalID: token.ApprovalID, Decision: "approved"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong run approval decision should be rejected, got %v", err)
	}
	if err := rt.DecideApproval(ctx, DecideApprovalCommand{
		RunID:      run.ID,
		ApprovalID: token.ApprovalID,
		DecidedBy:  "reviewer",
		Decision:   "approved",
		Reason:     "safe",
	}); err != nil {
		t.Fatalf("DecideApproval(approved) error = %v", err)
	}
	resumed := mustLoadTask(ctx, t, rt, run.ID, task.ID)
	if resumed.Status != TaskStatusDispatched {
		t.Fatalf("approved decision should redispatch paused task, got %#v", resumed)
	}
	running, err := rt.Run(ctx, run.ID)
	if err != nil {
		t.Fatalf("Run(resumed) error = %v", err)
	}
	if running.Status != RunStatusRunning {
		t.Fatalf("approved decision should resume run, got %#v", running)
	}
	eventTypes := collectEventTypes(rt.Events(context.Background(), run.ID))
	if !eventTypes.Contains(EventApprovalDecided) || !eventTypes.Contains(EventRunStatusChanged) {
		t.Fatalf("approval decision should emit decision and run-status events, got %#v", rt.Events(context.Background(), run.ID))
	}
}

func TestFailedReportRetriesThenFailsTask(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(ctx, t, rt, "run-report-retry")
	task := mustCreateTask(ctx, t, rt, CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "worker",
		OwnerAgentID: "agent-a",
		RetryPolicy:  RetryPolicy{MaxAttempts: 2, Backoff: time.Minute},
	})
	lease := leaseTask(ctx, t, rt, run.ID, task.ID, HolderAgent, "agent-a")

	if err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
		RunID:       run.ID,
		TaskID:      task.ID,
		LeaseID:     lease.ID,
		HolderType:  HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: task.Version,
		Report:      TypedReport{Status: ReportStatusFailed, Summary: "temporary", Retryable: true},
	}); err != nil {
		t.Fatalf("SubmitTypedReport(first failure) error = %v", err)
	}
	retryTask := mustLoadTask(ctx, t, rt, run.ID, task.ID)
	if retryTask.Status != TaskStatusDispatched || retryTask.Error != "temporary" || retryTask.Attempts != 1 {
		t.Fatalf("first failed report should expose dispatched retry attempt and error, got %#v", retryTask)
	}
	if active := mustActiveLeaseCount(context.Background(), t, rt, run.ID, task.ID); active != 0 {
		t.Fatalf("retry should release active lease, got %d", active)
	}

	retryEnvelopeID := lastDispatchedEnvelopeID(t, rt.Events(context.Background(), run.ID), task.ID)
	retryEnvelope := mustLoadEnvelope(ctx, t, rt, retryEnvelopeID)
	if retryEnvelope.NextRetryAt.IsZero() ||
		retryEnvelope.NextRetryAt.Before(retryEnvelope.CreatedAt.Add(time.Minute)) {
		t.Fatalf("retry envelope backoff not scheduled: %#v", retryEnvelope)
	}
	if retryEnvelope.Attempts != retryTask.Attempts {
		t.Fatalf("retry envelope attempts = %d, want %d", retryEnvelope.Attempts, retryTask.Attempts)
	}
	retryLease, acquired, err := rt.AcquireTaskExecution(ctx, AcquireTaskExecutionCommand{
		RunID:      run.ID,
		TaskID:     task.ID,
		EnvelopeID: retryEnvelopeID,
		HolderType: HolderAgent,
		HolderID:   "agent-a",
		TTL:        time.Minute,
	})
	if err != nil || !acquired {
		t.Fatalf("AcquireTaskExecution(retry) lease=%#v acquired=%v err=%v", retryLease, acquired, err)
	}
	runningRetry := mustLoadTask(ctx, t, rt, run.ID, task.ID)
	if err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
		RunID:       run.ID,
		TaskID:      task.ID,
		LeaseID:     retryLease.ID,
		HolderType:  HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: runningRetry.Version,
		Report:      TypedReport{Status: ReportStatusFailed, Summary: "permanent"},
	}); err != nil {
		t.Fatalf("SubmitTypedReport(second failure) error = %v", err)
	}
	failed := mustLoadTask(ctx, t, rt, run.ID, task.ID)
	if failed.Status != TaskStatusFailed || failed.Error != "permanent" {
		t.Fatalf("second failed report should fail task, got %#v", failed)
	}
	if active := mustActiveLeaseCount(context.Background(), t, rt, run.ID, task.ID); active != 0 {
		t.Fatalf("final failure should release active lease, got %d", active)
	}
}

func TestBlockedReportReleasesLease(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(ctx, t, rt, "run-blocked-report")
	task := mustCreateTask(ctx, t, rt, CreateTaskCommand{RunID: run.ID, TaskID: "worker", OwnerAgentID: "agent-a"})
	lease := leaseTask(ctx, t, rt, run.ID, task.ID, HolderAgent, "agent-a")
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
	blocked := mustLoadTask(ctx, t, rt, run.ID, task.ID)
	if blocked.Status != TaskStatusBlocked {
		t.Fatalf("blocked report should block task, got %#v", blocked)
	}
	if active := mustActiveLeaseCount(context.Background(), t, rt, run.ID, task.ID); active != 0 {
		t.Fatalf("blocked report must release active lease, got %d", active)
	}
}

func TestActionAttemptReconcileAndSourceIdentitySelector(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(ctx, t, rt, "run-action-selector")
	actionTask := mustCreateTask(ctx, t, rt, CreateTaskCommand{RunID: run.ID, TaskID: "action", Type: TaskTypeWorker, AllowsAction: true, OwnerAgentID: "agent-a"})
	actionLease := leaseTask(ctx, t, rt, run.ID, actionTask.ID, HolderAgent, "agent-a")
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
	reconcile := mustLoadTask(ctx, t, rt, run.ID, actionTask.ID)
	if reconcile.Status != TaskStatusReconcileRequired {
		t.Fatalf("unknown action attempt should require reconcile, got %#v", reconcile)
	}
	if active := mustActiveLeaseCount(context.Background(), t, rt, run.ID, actionTask.ID); active != 0 {
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
	run := mustStartRun(ctx, t, rt, "run-gateway")
	response := mustCreateTask(ctx, t, rt, CreateTaskCommand{RunID: run.ID, TaskID: "response", Type: TaskTypeResponse, OwnerComponent: "response_composer"})
	lease := leaseTask(ctx, t, rt, run.ID, response.ID, HolderComponent, "response_composer")
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
	message := mustResponseOutbox(context.Background(), t, rt, run.ID)[0]
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

func TestExecutionHeartbeatExtendsLease(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(ctx, t, rt, "run-execution-heartbeat")
	task := mustCreateTask(ctx, t, rt, CreateTaskCommand{RunID: run.ID, TaskID: "worker", OwnerAgentID: "agent-a"})
	lease := leaseTask(ctx, t, rt, run.ID, task.ID, HolderAgent, "agent-a")

	beforeHeartbeat := lease.ExpiresAt
	if err := rt.HeartbeatTaskExecution(ctx, HeartbeatTaskExecutionCommand{LeaseID: lease.ID, HolderID: "agent-a", TTL: 2 * time.Minute}); err != nil {
		t.Fatalf("HeartbeatTaskExecution() error = %v", err)
	}
	heartbeatLease := mustLoadLease(ctx, t, rt, lease.ID)
	if !heartbeatLease.ExpiresAt.After(beforeHeartbeat) {
		t.Fatalf("heartbeat did not extend lease: before=%s after=%s", beforeHeartbeat, heartbeatLease.ExpiresAt)
	}
	if !collectEventTypes(rt.Events(context.Background(), run.ID)).Contains(EventTaskExecutionHeartbeat) {
		t.Fatalf("heartbeat event missing, got %#v", rt.Events(context.Background(), run.ID))
	}
}

func TestExecutionReleaseRejectsWrongHolderAndStopsHeartbeat(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(ctx, t, rt, "run-execution-release")
	task := mustCreateTask(ctx, t, rt, CreateTaskCommand{RunID: run.ID, TaskID: "worker", OwnerAgentID: "agent-a"})
	lease := leaseTask(ctx, t, rt, run.ID, task.ID, HolderAgent, "agent-a")

	if err := rt.ReleaseTaskExecution(ctx, ReleaseTaskExecutionCommand{LeaseID: lease.ID, HolderID: "agent-b"}); !errors.Is(err, ErrLeaseHolderMismatch) {
		t.Fatalf("wrong holder release should be rejected, got %v", err)
	}
	if err := rt.ReleaseTaskExecution(ctx, ReleaseTaskExecutionCommand{LeaseID: lease.ID, HolderID: "agent-a"}); err != nil {
		t.Fatalf("ReleaseTaskExecution() error = %v", err)
	}
	if err := rt.HeartbeatTaskExecution(ctx, HeartbeatTaskExecutionCommand{LeaseID: lease.ID, HolderID: "agent-a", TTL: time.Minute}); !errors.Is(err, ErrLeaseNotActive) {
		t.Fatalf("heartbeat after release should fail with ErrLeaseNotActive, got %v", err)
	}
	if !collectEventTypes(rt.Events(context.Background(), run.ID)).Contains(EventTaskExecutionReleased) {
		t.Fatalf("release event missing, got %#v", rt.Events(context.Background(), run.ID))
	}
}

func TestTraceCommandsCloneMetadataAndRecordFailure(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(ctx, t, rt, "run-trace-command")
	task := mustCreateTask(ctx, t, rt, CreateTaskCommand{RunID: run.ID, TaskID: "worker", OwnerAgentID: "agent-a"})
	metadata := map[string]string{"phase": "start"}

	span, err := rt.StartTraceSpan(ctx, StartTraceSpanCommand{RunID: run.ID, TaskID: task.ID, Name: "worker.step", Component: "worker", Metadata: metadata})
	if err != nil {
		t.Fatalf("StartTraceSpan() error = %v", err)
	}
	metadata["phase"] = "mutated"
	if err := rt.EndTraceSpan(ctx, EndTraceSpanCommand{SpanID: span.ID, Error: "boom"}); err != nil {
		t.Fatalf("EndTraceSpan() error = %v", err)
	}
	got := lastTraceSpan(t, rt.TraceSpans(context.Background(), run.ID))
	if got.Status != TraceSpanFailed || got.Error != "boom" || got.Metadata["phase"] != "start" {
		t.Fatalf("trace span did not end with cloned metadata and failure: %#v", got)
	}
	eventTypes := collectEventTypes(rt.Events(context.Background(), run.ID))
	if !eventTypes.Contains(EventTraceSpanStarted) || !eventTypes.Contains(EventTraceSpanEnded) {
		t.Fatalf("trace events missing, got %#v", rt.Events(context.Background(), run.ID))
	}
}

func TestRequestApprovalPreservesMetadataAndRejectDecision(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(ctx, t, rt, "run-request-approval")
	task := mustCreateTask(ctx, t, rt, CreateTaskCommand{RunID: run.ID, TaskID: "approval", OwnerAgentID: "agent-a"})

	approval, token, err := requestApprovalWithMetadata(ctx, rt, run.ID, task.ID)
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}
	if approval.ActionID != "action-1" || approval.RiskSummary == "" || approval.RequestedAction != "deploy" || token.ApprovalID != approval.ApprovalID {
		t.Fatalf("approval request did not preserve action metadata: approval=%#v token=%#v", approval, token)
	}
	if err := rt.DecideApproval(ctx, DecideApprovalCommand{RunID: run.ID, ApprovalID: approval.ApprovalID, DecidedBy: "reviewer", Decision: "rejected", Reason: "too risky"}); err != nil {
		t.Fatalf("DecideApproval(rejected) error = %v", err)
	}
	decided := mustLoadApproval(ctx, t, rt, approval.ApprovalID)
	if decided.Status != "rejected" {
		t.Fatalf("rejected decision was not persisted: %#v", decided)
	}
	if !collectEventTypes(rt.Events(context.Background(), run.ID)).Contains(EventApprovalDecided) || !collectEventTypes(rt.Events(context.Background(), run.ID)).Contains(EventResumeTokenCreated) {
		t.Fatalf("approval request/decision events missing, got %#v", rt.Events(context.Background(), run.ID))
	}
}

func TestRecoverResumeTokenRejectsExpiredToken(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(ctx, t, rt, "run-expired-token")
	task := mustCreateTask(ctx, t, rt, CreateTaskCommand{RunID: run.ID, TaskID: "approval", OwnerAgentID: "agent-a"})
	_, token, err := requestApprovalWithMetadata(ctx, rt, run.ID, task.ID)
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}
	if _, err := rt.RecoverResumeToken(ctx, RecoverResumeTokenCommand{TokenID: token.TokenID}); err != nil {
		t.Fatalf("RecoverResumeToken(active) error = %v", err)
	}
	expireResumeToken(ctx, t, rt, token)
	if _, err := rt.RecoverResumeToken(ctx, RecoverResumeTokenCommand{TokenID: token.TokenID}); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("RecoverResumeToken(expired) should fail with ErrInvalidCommand, got %v", err)
	}
}

func TestPublishResponseFailureCommitsAuditWithoutPublishing(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(ctx, t, rt, "run-publish-failure")
	response := mustCreateTask(ctx, t, rt, CreateTaskCommand{RunID: run.ID, TaskID: "response", Type: TaskTypeResponse, OwnerComponent: "response_composer"})
	lease := leaseTask(ctx, t, rt, run.ID, response.ID, HolderComponent, "response_composer")
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
	message := mustResponseOutbox(context.Background(), t, rt, run.ID)[0]
	rt.SetOutputGateway(failingGateway{})
	if err := rt.PublishResponse(ctx, PublishResponseCommand{RunID: run.ID, MessageID: message.ID}); !errors.Is(err, errPublishFailed) {
		t.Fatalf("PublishResponse(failing gateway) error = %v, want %v", err, errPublishFailed)
	}
	after := mustResponseOutbox(context.Background(), t, rt, run.ID)[0]
	if after.Status != UserMessageQueued {
		t.Fatalf("failed publish must leave message queued, got %#v", after)
	}
	if !collectEventTypes(rt.Events(context.Background(), run.ID)).Contains(EventResponsePublishFailed) {
		t.Fatalf("failed publish should commit audit event, got %#v", rt.Events(context.Background(), run.ID))
	}
	rt.SetOutputGateway(nil)
	if err := rt.PublishResponse(ctx, PublishResponseCommand{RunID: run.ID, MessageID: message.ID}); err != nil {
		t.Fatalf("PublishResponse(retry) error = %v", err)
	}
	if err := rt.PublishResponse(ctx, PublishResponseCommand{RunID: run.ID, MessageID: message.ID}); err != nil {
		t.Fatalf("PublishResponse(idempotent) error = %v", err)
	}
}

func TestRuntimeConfigDefaultsAndOverrides(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Pipeline.Planner = staticPlanner{}
	cfg.Pipeline.Validator = staticValidator{}
	rt := New(cfg)
	if rt == nil {
		t.Fatalf("New(DefaultConfig override) returned nil")
	}
	rt.SetPolicyEngine(nil)
	rt.SetPipeline(PipelineComponents{Router: staticRouter{}, Dispatcher: staticDispatcher{}})
}

func TestRuntimeStoreDelegatesPersistRunsTasksEventsAndTraces(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run, task := seedStoreDelegateRunTask(ctx, t, rt, "run-store-delegates")

	if got, err := rt.LoadRun(ctx, run.ID); err != nil || got.ID != run.ID {
		t.Fatalf("LoadRun() = %#v err=%v", got, err)
	}
	if got, err := rt.LoadTask(ctx, run.ID, task.ID); err != nil || got.ID != task.ID {
		t.Fatalf("LoadTask() = %#v err=%v", got, err)
	}
	if tasks, err := rt.ListTasks(ctx, run.ID); err != nil || len(tasks) != 1 {
		t.Fatalf("ListTasks() = %#v err=%v", tasks, err)
	}

	event := Event{RunID: run.ID, TaskID: task.ID, Type: EventTaskCreated, RecordedAt: time.Now().UTC()}
	if err := rt.AppendEvent(ctx, event); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	if events, err := rt.ListEvents(ctx, run.ID); err != nil || len(events) != 1 {
		t.Fatalf("ListEvents() = %#v err=%v", events, err)
	}
	span := TraceSpan{ID: "span-store", RunID: run.ID, TaskID: task.ID, TraceID: "trace-store", Name: "store", Status: TraceSpanStarted, StartedAt: time.Now().UTC()}
	if err := rt.SaveTraceSpan(ctx, span); err != nil {
		t.Fatalf("SaveTraceSpan() error = %v", err)
	}
	if spans, err := rt.ListTraceSpans(ctx, run.ID); err != nil || len(spans) != 1 || spans[0].ID != span.ID {
		t.Fatalf("ListTraceSpans() = %#v err=%v", spans, err)
	}
}

func TestRuntimeStoreDelegatesPersistMessagesAndEnvelopes(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run, task := seedStoreDelegateRunTask(ctx, t, rt, "run-store-message-delegates")

	msg := UserMessage{ID: "msg-store", RunID: run.ID, TaskID: task.ID, Type: UserMessageTypeFinalAnswer, Payload: "hello", Status: UserMessageQueued, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := rt.QueueMessage(ctx, msg); err != nil {
		t.Fatalf("QueueMessage() error = %v", err)
	}
	msg.Status = UserMessagePublished
	if err := rt.UpdateMessage(ctx, msg); err != nil {
		t.Fatalf("UpdateMessage() error = %v", err)
	}
	if loaded, err := rt.LoadMessage(ctx, run.ID, msg.ID); err != nil || loaded.Status != UserMessagePublished {
		t.Fatalf("LoadMessage() = %#v err=%v", loaded, err)
	}
	if messages, err := rt.ListMessages(ctx, run.ID); err != nil || len(messages) != 1 {
		t.Fatalf("ListMessages() = %#v err=%v", messages, err)
	}
	if queued, err := rt.ListQueuedMessages(ctx); err != nil || len(queued) != 0 {
		t.Fatalf("ListQueuedMessages() = %#v err=%v", queued, err)
	}

	env := TaskEnvelope{ID: "env-store", RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a", Status: "pending", TaskVersion: task.Version, CreatedAt: time.Now().UTC()}
	if err := rt.QueueEnvelope(ctx, env); err != nil {
		t.Fatalf("QueueEnvelope() error = %v", err)
	}
	env.Status = "delivered"
	if err := rt.UpdateEnvelope(ctx, env); err != nil {
		t.Fatalf("UpdateEnvelope() error = %v", err)
	}
	if loaded, err := rt.LoadEnvelope(ctx, env.ID); err != nil || loaded.Status != "delivered" {
		t.Fatalf("LoadEnvelope() = %#v err=%v", loaded, err)
	}
	if envelopes, err := rt.ListEnvelopes(ctx, run.ID); err != nil || len(envelopes) != 1 {
		t.Fatalf("ListEnvelopes() = %#v err=%v", envelopes, err)
	}
}

func requestApprovalWithMetadata(ctx context.Context, rt *Runtime, runID, taskID string) (ApprovalRequest, ResumeToken, error) {
	return rt.RequestApproval(ctx, RequestApprovalCommand{
		RunID:            runID,
		TaskID:           taskID,
		ActionID:         "action-1",
		RequesterAgentID: "agent-a",
		Reason:           "deploy production",
		RiskSummary:      "external side effect",
		RequestedAction:  "deploy",
	})
}

func mustLoadLease(ctx context.Context, t *testing.T, rt *Runtime, leaseID string) TaskExecutionLease {
	t.Helper()
	uow, err := rt.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	lease, err := uow.Leases().LoadLease(ctx, leaseID)
	if err != nil {
		t.Fatalf("LoadLease(%s) error = %v", leaseID, err)
	}
	return lease
}

func mustLoadApproval(ctx context.Context, t *testing.T, rt *Runtime, approvalID string) ApprovalRequest {
	t.Helper()
	uow, err := rt.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	approval, err := uow.Approvals().LoadApproval(ctx, approvalID)
	if err != nil {
		t.Fatalf("LoadApproval(%s) error = %v", approvalID, err)
	}
	return approval
}

func expireResumeToken(ctx context.Context, t *testing.T, rt *Runtime, token ResumeToken) {
	t.Helper()
	uow, err := rt.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	token.ExpiresAt = time.Now().Add(-time.Minute)
	if err := uow.ResumeTokens().SaveResumeToken(ctx, token); err != nil {
		_ = uow.Rollback(ctx)
		t.Fatalf("SaveResumeToken(expired) error = %v", err)
	}
	if err := uow.Commit(ctx); err != nil {
		t.Fatalf("Commit(expired token) error = %v", err)
	}
}

func lastTraceSpan(t *testing.T, spans []TraceSpan) TraceSpan {
	t.Helper()
	if len(spans) == 0 {
		t.Fatalf("expected persisted trace span")
	}
	return spans[len(spans)-1]
}

func seedStoreDelegateRunTask(ctx context.Context, t *testing.T, rt *Runtime, runID string) (Run, Task) {
	t.Helper()
	run := Run{ID: runID, RootTaskID: "root", Status: RunStatusRunning, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	task := Task{ID: "worker", RunID: run.ID, Status: TaskStatusCreated, OwnerAgentID: "agent-a", Version: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := rt.SaveRun(ctx, run); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	if err := rt.SaveTask(ctx, task); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}
	return run, task
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

func (p recordingStoreProvider) Capabilities(ctx context.Context) (ports.StoreCapabilities, error) {
	return p.uow.store.StoreCapabilities(ctx)
}

func (p recordingStoreProvider) Begin(ctx context.Context) (UnitOfWork, error) {
	tx, err := p.uow.store.memProvider.Begin(ctx)
	if err != nil {
		return nil, err
	}
	p.uow.tx = tx
	p.uow.begun = true
	p.uow.committed = false
	p.uow.rolledBack = false
	return p.uow, nil
}

type recordingUnitOfWork struct {
	store      *Runtime
	tx         UnitOfWork
	begun      bool
	committed  bool
	rolledBack bool
}

func (u *recordingUnitOfWork) Runs() RunStore                    { return u.tx.Runs() }
func (u *recordingUnitOfWork) Tasks() TaskStore                  { return u.tx.Tasks() }
func (u *recordingUnitOfWork) Events() EventStore                { return u.tx.Events() }
func (u *recordingUnitOfWork) Blackboard() BlackboardStore       { return u.tx.Blackboard() }
func (u *recordingUnitOfWork) MailboxOutbox() MailboxOutboxStore { return u.tx.MailboxOutbox() }
func (u *recordingUnitOfWork) UserMessages() UserMessageStore    { return u.tx.UserMessages() }
func (u *recordingUnitOfWork) Trace() TraceStore                 { return u.tx.Trace() }
func (u *recordingUnitOfWork) Leases() LeaseStore                { return u.tx.Leases() }
func (u *recordingUnitOfWork) Approvals() ApprovalStore          { return u.tx.Approvals() }
func (u *recordingUnitOfWork) ResumeTokens() ResumeTokenStore    { return u.tx.ResumeTokens() }
func (u *recordingUnitOfWork) ActionAttempts() ActionAttemptStore {
	return u.tx.ActionAttempts()
}
func (u *recordingUnitOfWork) AgentProfiles() AgentProfileStore { return u.tx.AgentProfiles() }
func (u *recordingUnitOfWork) CapabilityCatalog() CapabilityStore {
	return u.tx.CapabilityCatalog()
}
func (u *recordingUnitOfWork) UsageRecords() UsageStore           { return u.tx.UsageRecords() }
func (u *recordingUnitOfWork) DeadLetters() DeadLetterStore       { return u.tx.DeadLetters() }
func (u *recordingUnitOfWork) Handoffs() HandoffStore             { return u.tx.Handoffs() }
func (u *recordingUnitOfWork) TeamStates() TeamStateStore         { return u.tx.TeamStates() }
func (u *recordingUnitOfWork) AgentInstances() AgentInstanceStore { return u.tx.AgentInstances() }

func (u *recordingUnitOfWork) Commit(ctx context.Context) error {
	u.committed = true
	return u.tx.Commit(ctx)
}

func (u *recordingUnitOfWork) Rollback(ctx context.Context) error {
	u.rolledBack = true
	if u.tx == nil {
		return nil
	}
	return u.tx.Rollback(ctx)
}

var errPublishFailed = errors.New("publish failed")

type failingGateway struct{}

func (failingGateway) Publish(context.Context, UserMessage) error {
	return errPublishFailed
}

type reentrantGateway struct {
	rt *Runtime
}

func (g reentrantGateway) Publish(_ context.Context, message UserMessage) error {
	_, _ = g.rt.ResponseOutbox(context.Background(), message.RunID)
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
	return defaultPipeline(PipelineComponents{}).TaskMonitor.DecideDeadLetter(context.Background(), env, reason)
}

func containsSpan(spans []TraceSpan, name string) bool {
	for _, span := range spans {
		if span.Name == name {
			return true
		}
	}
	return false
}

func assertPipelineOutcome(ctx context.Context, t *testing.T, rt *Runtime, policy *recordingPolicy, monitor *recordingMonitor, run Run) {
	t.Helper()
	if run.Status != RunStatusRunning {
		t.Fatalf("expected running after full pipeline, got %#v", run)
	}
	if !monitor.advanced {
		t.Fatalf("TaskMonitor.Advance was not called")
	}
	planned := mustLoadTask(ctx, t, rt, run.ID, "planned-worker")
	if planned.Status != TaskStatusDispatched || planned.OwnerAgentID != "agent-a" {
		t.Fatalf("pipeline did not persist and dispatch planned task: %#v", planned)
	}
	if !slices.Contains(policy.operations, PolicyOperationDispatch) {
		t.Fatalf("dispatch did not go through PolicyEngine, ops=%#v", policy.operations)
	}
	spans := rt.TraceSpans(context.Background(), run.ID)
	if !containsSpan(spans, "runtime.pipeline") || !containsSpan(spans, "policy.authorize.dispatch") || !containsSpan(spans, "mailbox.dispatch") {
		t.Fatalf("expected pipeline/policy/mailbox spans, got %#v", spans)
	}
}

func assertPolicyBlockedTask(ctx context.Context, t *testing.T, rt *Runtime, runID, taskID string, taskStatus TaskStatus, runStatus RunStatus) {
	t.Helper()
	task := mustLoadTask(ctx, t, rt, runID, taskID)
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
	if !collectEventTypes(rt.Events(context.Background(), runID)).Contains(EventTaskPaused) {
		t.Fatalf("policy effect did not emit TaskPaused, events=%#v", rt.Events(context.Background(), runID))
	}
}

func mustExecuteCommand(ctx context.Context, t *testing.T, rt *Runtime, cmd RuntimeCommand) any {
	t.Helper()
	result, err := rt.ExecuteCommand(ctx, cmd)
	if err != nil {
		t.Fatalf("ExecuteCommand(%s) error = %v", cmd.CommandName(), err)
	}
	return result
}

func mustAdvanceRun(ctx context.Context, t *testing.T, rt *Runtime, runID string) Run {
	t.Helper()
	run, err := rt.AdvanceRun(ctx, AdvanceRunCommand{RunID: runID})
	if err != nil {
		t.Fatalf("AdvanceRun() error = %v", err)
	}
	return run
}

func mustStartRun(ctx context.Context, t *testing.T, rt *Runtime, runID string) Run {
	t.Helper()
	run, _, err := rt.StartRun(ctx, StartRunCommand{RunID: runID, RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	return run
}

func mustCreateTask(ctx context.Context, t *testing.T, rt *Runtime, cmd CreateTaskCommand) Task {
	t.Helper()
	task, err := rt.CreateTask(ctx, cmd)
	if err != nil {
		t.Fatalf("CreateTask(%s) error = %v", cmd.TaskID, err)
	}
	return task
}

func mustDispatchTask(ctx context.Context, t *testing.T, rt *Runtime, cmd DispatchTaskCommand) TaskEnvelope {
	t.Helper()
	env, err := rt.DispatchTask(ctx, cmd)
	if err != nil {
		t.Fatalf("DispatchTask(%s) error = %v", cmd.TaskID, err)
	}
	return env
}

func mustLoadEnvelope(ctx context.Context, t *testing.T, rt *Runtime, envelopeID string) TaskEnvelope {
	t.Helper()
	env, err := rt.LoadEnvelope(ctx, envelopeID)
	if err != nil {
		t.Fatalf("LoadEnvelope(%s) error = %v", envelopeID, err)
	}
	return env
}

func mustLoadTask(ctx context.Context, t *testing.T, rt *Runtime, runID, taskID string) Task {
	t.Helper()
	task, err := rt.Task(ctx, runID, taskID)
	if err != nil {
		t.Fatalf("Task(%s) error = %v", taskID, err)
	}
	return task
}

func mustReadyTasks(ctx context.Context, t *testing.T, rt *Runtime, runID string) []Task {
	t.Helper()
	tasks, err := rt.ReadyTasks(ctx, runID)
	if err != nil {
		t.Fatalf("ReadyTasks() error = %v", err)
	}
	return tasks
}

func mustActiveLeaseCount(ctx context.Context, t *testing.T, rt *Runtime, runID, taskID string) int {
	t.Helper()
	n, err := rt.ActiveLeaseCount(ctx, runID, taskID)
	if err != nil {
		t.Fatalf("ActiveLeaseCount() error = %v", err)
	}
	return n
}

func mustResponseOutbox(ctx context.Context, t *testing.T, rt *Runtime, runID string) []UserMessage {
	t.Helper()
	messages, err := rt.ResponseOutbox(ctx, runID)
	if err != nil {
		t.Fatalf("ResponseOutbox() error = %v", err)
	}
	return messages
}

func mustResumeTokens(ctx context.Context, t *testing.T, rt *Runtime) map[string]ResumeToken {
	t.Helper()
	tokens, err := rt.ResumeTokens(ctx)
	if err != nil {
		t.Fatalf("ResumeTokens() error = %v", err)
	}
	return tokens
}
