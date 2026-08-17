package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLeaseReportAndMailboxContracts(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run, _, err := rt.StartRun(ctx, StartRunCommand{
		RunID:      "run-lease",
		RootTaskID: "root",
		Request:    "answer the user",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := rt.CreateTask(ctx, CreateTaskCommand{
		RunID:              run.ID,
		TaskID:             "worker-1",
		Type:               TaskTypeWorker,
		AssignedAgentID:    "agent-a",
		OwnerAgentID:       "agent-a",
		CompletionCriteria: []string{"produce report"},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	dependent, err := rt.CreateTask(ctx, CreateTaskCommand{
		RunID:          run.ID,
		TaskID:         "synthesis-1",
		Type:           TaskTypeWorker,
		Tags:           []string{"synthesis"},
		OwnerComponent: "synthesizer",
		DependsOn:      []string{task.ID},
	})
	if err != nil {
		t.Fatalf("CreateTask(dependent) error = %v", err)
	}
	env, err := rt.DispatchTask(ctx, DispatchTaskCommand{
		RunID:         run.ID,
		TaskID:        task.ID,
		TargetAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}

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
	if _, duplicate, err := rt.AcquireTaskExecution(ctx, AcquireTaskExecutionCommand{
		RunID:      run.ID,
		TaskID:     task.ID,
		EnvelopeID: env.ID,
		HolderType: HolderAgent,
		HolderID:   "agent-a",
		TTL:        time.Minute,
	}); err != nil || duplicate {
		t.Fatalf("duplicate AcquireTaskExecution() acquired=%v err=%v", duplicate, err)
	}
	if got := mustActiveLeaseCount(context.Background(), t, rt, run.ID, task.ID); got != 1 {
		t.Fatalf("expected one active lease, got %d", got)
	}

	if err := rt.AckEnvelope(ctx, AckEnvelopeCommand{EnvelopeID: env.ID, HolderID: "agent-a"}); err != nil {
		t.Fatalf("AckEnvelope() error = %v", err)
	}
	afterAck, err := rt.Task(ctx, run.ID, task.ID)
	if err != nil {
		t.Fatalf("Task() error = %v", err)
	}
	if afterAck.Status == TaskStatusCompleted || afterAck.OwnerAgentID != "agent-a" {
		t.Fatalf("mailbox ack changed task state/owner: %#v", afterAck)
	}

	if err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
		RunID:       run.ID,
		TaskID:      task.ID,
		LeaseID:     lease.ID,
		HolderType:  HolderAgent,
		HolderID:    "agent-b",
		TaskVersion: task.Version,
		Report:      TypedReport{Status: ReportStatusSuccess, Summary: "wrong holder"},
	}); !errors.Is(err, ErrLeaseHolderMismatch) {
		t.Fatalf("expected ErrLeaseHolderMismatch, got %v", err)
	}
	if err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
		RunID:       run.ID,
		TaskID:      task.ID,
		LeaseID:     lease.ID,
		HolderType:  HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: task.Version + 1,
		Report:      TypedReport{Status: ReportStatusSuccess, Summary: "stale version"},
	}); !errors.Is(err, ErrStaleTaskVersion) {
		t.Fatalf("expected ErrStaleTaskVersion, got %v", err)
	}
	if err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
		RunID:       run.ID,
		TaskID:      task.ID,
		LeaseID:     lease.ID,
		HolderType:  HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: task.Version,
		Report:      TypedReport{Status: ReportStatusPartialSuccess, Summary: "partial"},
	}); err != nil {
		t.Fatalf("SubmitTypedReport(partial) error = %v", err)
	}
	partial, err := rt.Task(ctx, run.ID, task.ID)
	if err != nil {
		t.Fatalf("Task(partial) error = %v", err)
	}
	if partial.Status != TaskStatusRunning {
		t.Fatalf("partial_success must keep task running, got %#v", partial)
	}
	if ready := mustReadyTasks(context.Background(), t, rt, run.ID); containsTask(ready, dependent.ID) {
		t.Fatalf("partial_success satisfied downstream dependency: %#v", ready)
	}

	if err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
		RunID:       run.ID,
		TaskID:      task.ID,
		LeaseID:     lease.ID,
		HolderType:  HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: partial.Version,
		Report:      TypedReport{Status: ReportStatusSuccess, Summary: "done: produce report"},
	}); err != nil {
		t.Fatalf("SubmitTypedReport(success) error = %v", err)
	}
	completed, err := rt.Task(ctx, run.ID, task.ID)
	if err != nil {
		t.Fatalf("Task(completed) error = %v", err)
	}
	if completed.Status != TaskStatusCompleted {
		t.Fatalf("expected completed task, got %#v", completed)
	}
	if err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
		RunID:       run.ID,
		TaskID:      task.ID,
		LeaseID:     lease.ID,
		HolderType:  HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: completed.Version,
		Report:      TypedReport{Status: ReportStatusSuccess, Summary: "late"},
	}); !errors.Is(err, ErrTerminalState) {
		t.Fatalf("expected terminal task report to be rejected, got %v", err)
	}
}

func TestAcquireRejectsMismatchedEnvelopeWithoutMutatingTaskOrEnvelope(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run, _, err := rt.StartRun(ctx, StartRunCommand{RunID: "run-envelope", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	taskA, err := rt.CreateTask(ctx, CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "task-a",
		OwnerAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("CreateTask(task-a) error = %v", err)
	}
	taskB, err := rt.CreateTask(ctx, CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "task-b",
		OwnerAgentID: "agent-b",
	})
	if err != nil {
		t.Fatalf("CreateTask(task-b) error = %v", err)
	}
	envB, err := rt.DispatchTask(ctx, DispatchTaskCommand{
		RunID:         run.ID,
		TaskID:        taskB.ID,
		TargetAgentID: "agent-b",
	})
	if err != nil {
		t.Fatalf("DispatchTask(task-b) error = %v", err)
	}

	if _, acquired, err := rt.AcquireTaskExecution(ctx, AcquireTaskExecutionCommand{
		RunID:      run.ID,
		TaskID:     taskA.ID,
		EnvelopeID: envB.ID,
		HolderType: HolderAgent,
		HolderID:   "agent-a",
		TTL:        time.Minute,
	}); !errors.Is(err, ErrLeaseHolderMismatch) || acquired {
		t.Fatalf("expected mismatched envelope rejection, acquired=%v err=%v", acquired, err)
	}
	afterTaskA, err := rt.Task(ctx, run.ID, taskA.ID)
	if err != nil {
		t.Fatalf("Task(task-a) error = %v", err)
	}
	if afterTaskA.Status != taskA.Status || afterTaskA.Attempts != taskA.Attempts {
		t.Fatalf("mismatched envelope mutated task: before=%#v after=%#v", taskA, afterTaskA)
	}
	envBAfter, err := rt.LoadEnvelope(ctx, envB.ID)
	if err != nil {
		t.Fatalf("LoadEnvelope(env-b) error = %v", err)
	}
	if envBAfter.Status != "pending" {
		t.Fatalf("mismatched acquire delivered another task's envelope, status=%q", envBAfter.Status)
	}

	if _, acquired, err := rt.AcquireTaskExecution(ctx, AcquireTaskExecutionCommand{
		RunID:      run.ID,
		TaskID:     taskA.ID,
		EnvelopeID: "missing-envelope",
		HolderType: HolderAgent,
		HolderID:   "agent-a",
		TTL:        time.Minute,
	}); !errors.Is(err, ErrNotFound) || acquired {
		t.Fatalf("expected missing envelope rejection, acquired=%v err=%v", acquired, err)
	}
	afterMissing, err := rt.Task(ctx, run.ID, taskA.ID)
	if err != nil {
		t.Fatalf("Task(task-a after missing envelope) error = %v", err)
	}
	if afterMissing.Status != taskA.Status || afterMissing.Attempts != taskA.Attempts {
		t.Fatalf("missing envelope mutated task: before=%#v after=%#v", taskA, afterMissing)
	}
}

func TestDispatchRejectsUnmetDependencies(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run, _, err := rt.StartRun(ctx, StartRunCommand{RunID: "run-deps", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	dependency, err := rt.CreateTask(ctx, CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "dependency",
		OwnerAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("CreateTask(dependency) error = %v", err)
	}
	dependent, err := rt.CreateTask(ctx, CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "dependent",
		OwnerAgentID: "agent-a",
		DependsOn:    []string{dependency.ID},
	})
	if err != nil {
		t.Fatalf("CreateTask(dependent) error = %v", err)
	}
	if _, err := rt.DispatchTask(ctx, DispatchTaskCommand{
		RunID:         run.ID,
		TaskID:        dependent.ID,
		TargetAgentID: "agent-a",
	}); !errors.Is(err, ErrDependencyUnmet) {
		t.Fatalf("expected unmet dependency rejection, got %v", err)
	}
	after, err := rt.Task(ctx, run.ID, dependent.ID)
	if err != nil {
		t.Fatalf("Task(dependent) error = %v", err)
	}
	if after.Status != TaskStatusWaitingDependency {
		t.Fatalf("unmet dependency dispatch mutated task status: %#v", after)
	}
}

func TestActionToolAndClarificationContracts(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run, _, err := rt.StartRun(ctx, StartRunCommand{RunID: "run-action", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	rt.RegisterTool(Tool{
		Name:               "deploy",
		EffectType:         ToolEffectExternalSideEffect,
		RequiresActionTask: true,
		RiskLevel:          "high",
		Idempotent:         false,
	})
	worker, err := rt.CreateTask(ctx, CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "worker-1",
		Type:         TaskTypeWorker,
		OwnerAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("CreateTask(worker) error = %v", err)
	}
	if _, err := rt.InvokeTool(ctx, ToolInvocation{RunID: run.ID, TaskID: worker.ID, ToolName: "deploy"}); !errors.Is(err, ErrActionTaskRequired) {
		t.Fatalf("expected side-effecting worker tool to be blocked, got %v", err)
	}

	clarificationLease := leaseTask(ctx, t, rt, run.ID, worker.ID, HolderAgent, "agent-a")
	if err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
		RunID:       run.ID,
		TaskID:      worker.ID,
		LeaseID:     clarificationLease.ID,
		HolderType:  HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: worker.Version,
		Report:      TypedReport{Status: ReportStatusNeedsClarification, Summary: "need region"},
	}); err != nil {
		t.Fatalf("SubmitTypedReport(needs_clarification) error = %v", err)
	}
	blocked, err := rt.Task(ctx, run.ID, worker.ID)
	if err != nil {
		t.Fatalf("Task(blocked) error = %v", err)
	}
	currentRun, err := rt.Run(ctx, run.ID)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if blocked.Status != TaskStatusWaitingUserInput || currentRun.Status != RunStatusWaitingUserInput {
		t.Fatalf("needs_clarification did not block task/run: task=%#v run=%#v", blocked, currentRun)
	}
	if !collectEventTypes(rt.Events(context.Background(), run.ID)).Contains(EventSystemResponseBypassAudited) {
		t.Fatalf("system clarification response should emit bypass audit event, events=%#v", rt.Events(context.Background(), run.ID))
	}
	if active := mustActiveLeaseCount(context.Background(), t, rt, run.ID, worker.ID); active != 0 {
		t.Fatalf("needs_clarification must release active lease, got %d", active)
	}
	if err := rt.SubmitUserInput(ctx, SubmitUserInputCommand{RunID: run.ID, TaskID: worker.ID, Input: "region=us-east-1"}); err != nil {
		t.Fatalf("SubmitUserInput() error = %v", err)
	}
	resumedRun, err := rt.Run(ctx, run.ID)
	if err != nil {
		t.Fatalf("Run(resumed) error = %v", err)
	}
	if resumedRun.Status != RunStatusRunning {
		t.Fatalf("expected run to resume running, got %#v", resumedRun)
	}
	resumedTask, err := rt.Task(ctx, run.ID, worker.ID)
	if err != nil {
		t.Fatalf("Task(resumed) error = %v", err)
	}
	if resumedTask.Status != TaskStatusDispatched {
		t.Fatalf("SubmitUserInput must redispatch instead of running task directly, got %#v", resumedTask)
	}

	action, err := rt.CreateTask(ctx, CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "action-1",
		Type:         TaskTypeWorker,
		AllowsAction: true,
		OwnerAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("CreateTask(action) error = %v", err)
	}
	actionLease := leaseTask(ctx, t, rt, run.ID, action.ID, HolderAgent, "agent-a")
	if err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
		RunID:       run.ID,
		TaskID:      action.ID,
		LeaseID:     actionLease.ID,
		HolderType:  HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: action.Version,
		Report: TypedReport{
			Status:        ReportStatusSuccess,
			Summary:       "deployed",
			ActionOutcome: &ActionOutcome{AttemptID: "attempt-1", Status: ActionAttemptSucceeded, Output: "ok"},
		},
	}); err != nil {
		t.Fatalf("SubmitTypedReport(action success) error = %v", err)
	}
	completedAction, err := rt.Task(ctx, run.ID, action.ID)
	if err != nil {
		t.Fatalf("Task(action completed) error = %v", err)
	}
	if completedAction.Status != TaskStatusCompleted {
		t.Fatalf("expected action task completion through typed report, got %#v", completedAction)
	}

	failedAction, err := rt.CreateTask(ctx, CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "action-failed",
		Type:         TaskTypeWorker,
		AllowsAction: true,
		OwnerAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("CreateTask(action failed) error = %v", err)
	}
	failedLease := leaseTask(ctx, t, rt, run.ID, failedAction.ID, HolderAgent, "agent-a")
	if err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
		RunID:       run.ID,
		TaskID:      failedAction.ID,
		LeaseID:     failedLease.ID,
		HolderType:  HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: failedAction.Version,
		Report: TypedReport{
			Status:        ReportStatusSuccess,
			Summary:       "attempt failed",
			ActionOutcome: &ActionOutcome{AttemptID: "attempt-failed", Status: ActionAttemptFailed, Error: "denied"},
		},
	}); err != nil {
		t.Fatalf("SubmitTypedReport(action failed) error = %v", err)
	}
	failedAfter, err := rt.Task(ctx, run.ID, failedAction.ID)
	if err != nil {
		t.Fatalf("Task(action failed) error = %v", err)
	}
	if failedAfter.Status != TaskStatusFailed || failedAfter.Error != "denied" {
		t.Fatalf("failed action result must not complete task, got %#v", failedAfter)
	}

	unknownAction, err := rt.CreateTask(ctx, CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "action-unknown",
		Type:         TaskTypeWorker,
		AllowsAction: true,
		OwnerAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("CreateTask(action unknown) error = %v", err)
	}
	unknownLease := leaseTask(ctx, t, rt, run.ID, unknownAction.ID, HolderAgent, "agent-a")
	if err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
		RunID:       run.ID,
		TaskID:      unknownAction.ID,
		LeaseID:     unknownLease.ID,
		HolderType:  HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: unknownAction.Version,
		Report: TypedReport{
			Status:        ReportStatusSuccess,
			Summary:       "unknown",
			ActionOutcome: &ActionOutcome{AttemptID: "attempt-unknown", Status: ActionAttemptUnknown},
		},
	}); !errors.Is(err, ErrActionReconcileRequired) {
		t.Fatalf("expected unknown action reconcile error, got %v", err)
	}
	reconcileTask, err := rt.Task(ctx, run.ID, unknownAction.ID)
	if err != nil {
		t.Fatalf("Task(action unknown) error = %v", err)
	}
	if reconcileTask.Status != TaskStatusReconcileRequired || reconcileTask.Attempts != 1 {
		t.Fatalf("unknown action must block without auto retry, got %#v", reconcileTask)
	}
	if active := mustActiveLeaseCount(context.Background(), t, rt, run.ID, unknownAction.ID); active != 0 {
		t.Fatalf("reconcile_required must release active lease, got %d", active)
	}
	if !collectEventTypes(rt.Events(context.Background(), run.ID)).Contains(EventActionReconcileRequired) {
		t.Fatalf("expected ActionReconcileRequired event, got %#v", rt.Events(context.Background(), run.ID))
	}
}

func TestHandoffPolicyResponseReplayAndFlowContracts(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	rt.SetMessagePolicy(func(message UserMessage) PolicyDecision {
		return PolicyDecision{
			DecisionID: "decision-1",
			Effect:     PolicyEffectAllow,
			Obligations: []PolicyObligation{
				{Kind: ObligationRedactFields, Target: PolicyTargetResponse},
				{Kind: ObligationHideInternalTrace, Target: PolicyTargetResponse},
			},
			Redactions: []string{"email"},
		}
	})
	run, _, err := rt.StartRun(ctx, StartRunCommand{RunID: "run-response", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	worker, err := rt.CreateTask(ctx, CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "worker-1",
		Type:         TaskTypeWorker,
		OwnerAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("CreateTask(worker) error = %v", err)
	}
	if err := rt.RequestHandoff(ctx, HandoffCommand{
		RunID:          run.ID,
		TaskID:         worker.ID,
		FromAgentID:    "agent-a",
		ToAgentID:      "agent-b",
		TaskVersion:    worker.Version,
		HandoffContext: "agent-a gathered facts",
	}); err != nil {
		t.Fatalf("RequestHandoff() error = %v", err)
	}
	handedOff, err := rt.Task(ctx, run.ID, worker.ID)
	if err != nil {
		t.Fatalf("Task(handed off) error = %v", err)
	}
	if handedOff.OwnerAgentID != "agent-b" || handedOff.HandoffCount != 1 {
		t.Fatalf("handoff did not transfer ownership: %#v", handedOff)
	}
	events := rt.Events(context.Background(), run.ID)
	requestedIdx := indexEvent(events, EventHandoffRequested)
	contextIdx := indexEvent(events, EventBlackboardItemWritten)
	ownerIdx := indexEvent(events, EventTaskOwnerChanged)
	appliedIdx := indexEvent(events, EventHandoffApplied)
	queuedIdx := indexEvent(events, EventHandoffEnvelopeQueued)
	if requestedIdx < 0 || contextIdx < 0 || ownerIdx < 0 || appliedIdx < 0 || queuedIdx < 0 ||
		!(requestedIdx < contextIdx && contextIdx < ownerIdx && ownerIdx < appliedIdx && appliedIdx < queuedIdx) {
		t.Fatalf("handoff event order is invalid, events=%#v", events)
	}
	if err := rt.RequestHandoff(ctx, HandoffCommand{
		RunID:       run.ID,
		TaskID:      worker.ID,
		FromAgentID: "agent-a",
		ToAgentID:   "agent-c",
		TaskVersion: worker.Version,
	}); !errors.Is(err, ErrStaleTaskVersion) {
		t.Fatalf("expected stale handoff to be rejected, got %v", err)
	}

	response, err := rt.CreateTask(ctx, CreateTaskCommand{
		RunID:          run.ID,
		TaskID:         "response-1",
		Type:           TaskTypeResponse,
		OwnerComponent: "response_composer",
	})
	if err != nil {
		t.Fatalf("CreateTask(response) error = %v", err)
	}
	if err := rt.SubmitResponseOutput(ctx, SubmitResponseOutputCommand{
		RunID:       run.ID,
		TaskID:      worker.ID,
		HolderType:  HolderAgent,
		HolderID:    "agent-b",
		TaskVersion: handedOff.Version,
		Payload:     "raw user@example.com",
	}); !errors.Is(err, ErrResponseTaskRequired) {
		t.Fatalf("expected only response tasks to compose output, got %v", err)
	}
	responseLease := leaseTask(ctx, t, rt, run.ID, response.ID, HolderComponent, "response_composer")
	if err := rt.SubmitResponseOutput(ctx, SubmitResponseOutputCommand{
		RunID:       run.ID,
		TaskID:      response.ID,
		LeaseID:     responseLease.ID,
		HolderType:  HolderComponent,
		HolderID:    "response_composer",
		TaskVersion: response.Version,
		Payload:     "send to user@example.com\ninternal trace: keep private",
	}); err != nil {
		t.Fatalf("SubmitResponseOutput() error = %v", err)
	}
	queued := mustResponseOutbox(context.Background(), t, rt, run.ID)
	if len(queued) != 1 {
		t.Fatalf("expected one queued response, got %#v", queued)
	}
	if strings.Contains(queued[0].Payload, "user@example.com") || strings.Contains(queued[0].Payload, "internal trace") {
		t.Fatalf("ResponseOutbox stored unsanitized payload: %#v", queued[0])
	}
	if queued[0].Status != UserMessageQueued {
		t.Fatalf("expected queued response, got %#v", queued[0])
	}
	for _, event := range rt.Events(context.Background(), run.ID) {
		if event.Type != EventUserMessageComposed {
			continue
		}
		payload := stringFromPayload(mapFromPayload(event.Payload["message"])["payload"])
		if strings.Contains(payload, "user@example.com") || strings.Contains(payload, "internal trace") {
			t.Fatalf("composed response event leaked unsanitized payload: %#v", event)
		}
	}
	if err := rt.PublishResponse(ctx, PublishResponseCommand{RunID: run.ID, MessageID: queued[0].ID}); err != nil {
		t.Fatalf("PublishResponse() error = %v", err)
	}
	published := mustResponseOutbox(context.Background(), t, rt, run.ID)
	if published[0].Status != UserMessagePublished {
		t.Fatalf("OutputGateway did not mark response published: %#v", published[0])
	}

	rt.SetMessagePolicy(func(UserMessage) PolicyDecision {
		return PolicyDecision{
			DecisionID: "decision-fail",
			Effect:     PolicyEffectAllow,
			Obligations: []PolicyObligation{
				{Kind: "unsupported_obligation", Target: PolicyTargetResponse},
			},
		}
	})
	failing, err := rt.CreateTask(ctx, CreateTaskCommand{
		RunID:          run.ID,
		TaskID:         "response-failing",
		Type:           TaskTypeResponse,
		OwnerComponent: "response_composer",
	})
	if err != nil {
		t.Fatalf("CreateTask(failing response) error = %v", err)
	}
	failingLease := leaseTask(ctx, t, rt, run.ID, failing.ID, HolderComponent, "response_composer")
	if err := rt.SubmitResponseOutput(ctx, SubmitResponseOutputCommand{
		RunID:       run.ID,
		TaskID:      failing.ID,
		LeaseID:     failingLease.ID,
		HolderType:  HolderComponent,
		HolderID:    "response_composer",
		TaskVersion: failing.Version,
		Payload:     "hello",
	}); !errors.Is(err, ErrPolicyObligationFailed) {
		t.Fatalf("expected policy obligation failure, got %v", err)
	}
	if !collectEventTypes(rt.Events(context.Background(), run.ID)).Contains(EventPolicyObligationFailed) {
		t.Fatalf("expected PolicyObligationFailed event, got %#v", rt.Events(context.Background(), run.ID))
	}

	if err := rt.RegisterFlow(Flow{Name: "smoke", PlannerPreset: "default"}); err != nil {
		t.Fatalf("expected flow registration to succeed, got %v", err)
	}
	projection, err := rt.Replay(context.Background(), run.ID, ReplayModeAudit)
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if projection.SideEffects.MailboxDeliveries != 0 || projection.SideEffects.UserMessagePublications != 0 || projection.SideEffects.ActionExecutions != 0 {
		t.Fatalf("audit replay performed side effects: %#v", projection.SideEffects)
	}

	env, err := rt.DispatchTask(ctx, DispatchTaskCommand{RunID: run.ID, TaskID: handedOff.ID, TargetAgentID: "agent-b"})
	if err != nil {
		t.Fatalf("DispatchTask(handoff owner) error = %v", err)
	}
	if err := rt.DeadLetter(ctx, DeadLetterCommand{EnvelopeID: env.ID, Reason: "delivery exhausted"}); err != nil {
		t.Fatalf("DeadLetter() error = %v", err)
	}
	deadTask, err := rt.Task(ctx, run.ID, handedOff.ID)
	if err != nil {
		t.Fatalf("Task(dead-letter) error = %v", err)
	}
	if deadTask.Status != TaskStatusBlocked {
		t.Fatalf("dead-letter must trigger monitor decision and block task, got %#v", deadTask)
	}
	if !collectEventTypes(rt.Events(context.Background(), run.ID)).Contains(EventTaskMonitorDecision) {
		t.Fatalf("expected TaskMonitorDecision after dead-letter, got %#v", rt.Events(context.Background(), run.ID))
	}
}

func TestSelectItemsAppliesCompleteSelector(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run, _, err := rt.StartRun(ctx, StartRunCommand{RunID: "run-selector", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	items := []BlackboardItem{
		{
			RunID:      run.ID,
			TaskID:     "task-a",
			Type:       BlackboardItemClaim,
			Source:     SourceIdentity{Type: SourceAgent, ID: "agent-a"},
			Visibility: BlackboardVisibilityAgentVisible,
			Key:        "wanted",
			Payload:    "old version",
			Version:    1,
		},
		{
			RunID:      run.ID,
			TaskID:     "task-a",
			Type:       BlackboardItemClaim,
			Source:     SourceIdentity{Type: SourceAgent, ID: "agent-b"},
			Visibility: BlackboardVisibilityAgentVisible,
			Key:        "wanted",
			Payload:    "wrong agent",
			Version:    3,
		},
		{
			RunID:      run.ID,
			TaskID:     "task-a",
			Type:       BlackboardItemClaim,
			Source:     SourceIdentity{Type: SourceAgent, ID: "agent-a"},
			Visibility: BlackboardVisibilityAgentVisible,
			Key:        "ignored",
			Payload:    "wrong key",
			Version:    3,
		},
		{
			RunID:      run.ID,
			TaskID:     "task-a",
			Type:       BlackboardItemClaim,
			Source:     SourceIdentity{Type: SourceAgent, ID: "agent-a"},
			Visibility: BlackboardVisibilityAgentVisible,
			Key:        "wanted",
			Payload:    "selected",
			Version:    3,
		},
	}
	for _, item := range items {
		if err := rt.WriteItem(ctx, item); err != nil {
			t.Fatalf("WriteItem() error = %v", err)
		}
	}

	selected, err := rt.SelectItems(ctx, run.ID, BlackboardSelector{
		TaskID:       "task-a",
		ItemTypes:    []BlackboardItemType{BlackboardItemClaim},
		SourceTypes:  []SourceType{SourceAgent},
		SourceIDs:    []string{"agent-a"},
		Visibility:   BlackboardVisibilityAgentVisible,
		SinceVersion: 2,
		Keys:         []string{"wanted"},
	})
	if err != nil {
		t.Fatalf("SelectItems() error = %v", err)
	}
	if len(selected) != 1 || selected[0].Payload != "selected" {
		t.Fatalf("expected only complete selector match, got %#v", selected)
	}
}

func containsTask(tasks []Task, id string) bool {
	for _, task := range tasks {
		if task.ID == id {
			return true
		}
	}
	return false
}

func leaseTask(ctx context.Context, t *testing.T, rt *Runtime, runID, taskID string, holderType HolderType, holderID string) TaskExecutionLease {
	t.Helper()
	task, err := rt.Task(ctx, runID, taskID)
	if err != nil {
		t.Fatalf("Task(%s) error = %v", taskID, err)
	}
	env, err := rt.DispatchTask(ctx, DispatchTaskCommand{
		RunID:           runID,
		TaskID:          taskID,
		TargetAgentID:   task.OwnerAgentID,
		TargetComponent: task.OwnerComponent,
	})
	if err != nil {
		t.Fatalf("DispatchTask(%s) error = %v", taskID, err)
	}
	lease, ok, err := rt.AcquireTaskExecution(ctx, AcquireTaskExecutionCommand{
		RunID:      runID,
		TaskID:     taskID,
		EnvelopeID: env.ID,
		HolderType: holderType,
		HolderID:   holderID,
		TTL:        time.Minute,
	})
	if err != nil || !ok {
		t.Fatalf("AcquireTaskExecution(%s) lease=%#v ok=%v err=%v", taskID, lease, ok, err)
	}
	return lease
}

type eventTypes []EventType

func (types eventTypes) Contains(want EventType) bool {
	for _, got := range types {
		if got == want {
			return true
		}
	}
	return false
}

func collectEventTypes(events []Event) eventTypes {
	out := make([]EventType, 0, len(events))
	for _, event := range events {
		out = append(out, event.Type)
	}
	return out
}

func indexEvent(events []Event, typ EventType) int {
	for idx, event := range events {
		if event.Type == typ {
			return idx
		}
	}
	return -1
}

func lastDispatchedEnvelopeID(t *testing.T, events []Event, taskID string) string {
	t.Helper()
	for idx := len(events) - 1; idx >= 0; idx-- {
		event := events[idx]
		if event.Type != EventTaskDispatched || event.TaskID != taskID {
			continue
		}
		envelope, ok := event.Payload["envelope"].(map[string]any)
		if !ok {
			t.Fatalf("TaskDispatched event missing envelope payload: %#v", event)
		}
		id, _ := envelope["envelopeId"].(string)
		return id
	}
	t.Fatalf("missing TaskDispatched event for task %s in %#v", taskID, events)
	return ""
}

func lastQueuedMessageID(t *testing.T, events []Event) string {
	t.Helper()
	for idx := len(events) - 1; idx >= 0; idx-- {
		event := events[idx]
		if event.Type != EventUserMessageQueued {
			continue
		}
		id, _ := event.Payload["messageId"].(string)
		return id
	}
	t.Fatalf("missing UserMessageQueued event in %#v", events)
	return ""
}
