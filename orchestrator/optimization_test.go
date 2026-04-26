package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStartRunCreatesCreatedStateAndPipelineAdvancesThroughStages(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()

	run, root, err := rt.StartRun(ctx, StartRunCommand{
		RunID:      "run-pipeline",
		RootTaskID: "root",
		Request:    "plan the work",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	if run.Status != RunStatusCreated {
		t.Fatalf("StartRun must leave run at created, got %#v", run)
	}
	if root.Status != TaskStatusCreated {
		t.Fatalf("root task must start created, got %#v", root)
	}
	if err := rt.TransitionRun(ctx, TransitionRunCommand{RunID: run.ID, To: RunStatusCompleted}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected invalid direct created->completed transition, got %v", err)
	}

	advanced, err := rt.AdvanceRun(ctx, AdvanceRunCommand{RunID: run.ID})
	if err != nil {
		t.Fatalf("AdvanceRun() error = %v", err)
	}
	if advanced.Status != RunStatusRunning {
		t.Fatalf("pipeline should advance run to running, got %#v", advanced)
	}
	runEvents, err := rt.RunEvents(ctx, run.ID)
	if err != nil {
		t.Fatalf("RunEvents() error = %v", err)
	}
	events := collectEventTypes(runEvents)
	for _, want := range []EventType{
		EventRunStatusChanged,
		EventIntentAnalyzed,
		EventPlanCreated,
		EventPlanValidated,
		EventRoutingPlanCreated,
		EventTaskDispatched,
	} {
		if !events.Contains(want) {
			t.Fatalf("missing pipeline event %s in %#v", want, runEvents)
		}
	}
}

func TestTypedReportCompletionRetryHandoffAndClarificationSemantics(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run, _, err := rt.StartRun(ctx, StartRunCommand{RunID: "run-report", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	criteria, err := rt.CreateTask(ctx, CreateTaskCommand{
		RunID:              run.ID,
		TaskID:             "criteria",
		Type:               TaskTypeWorker,
		OwnerAgentID:       "agent-a",
		CompletionCriteria: []string{"must include final answer"},
	})
	if err != nil {
		t.Fatalf("CreateTask(criteria) error = %v", err)
	}
	criteriaLease := leaseTask(t, ctx, rt, run.ID, criteria.ID, HolderAgent, "agent-a")
	if err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
		RunID:       run.ID,
		TaskID:      criteria.ID,
		LeaseID:     criteriaLease.ID,
		HolderType:  HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: criteria.Version,
		Report:      TypedReport{Status: ReportStatusSuccess, Summary: "draft only"},
	}); !errors.Is(err, ErrCompletionCriteriaUnmet) {
		t.Fatalf("expected unmet completion criteria rejection, got %v", err)
	}
	criteriaAfter, err := rt.Task(ctx, run.ID, criteria.ID)
	if err != nil {
		t.Fatalf("Task(criteria) error = %v", err)
	}
	if criteriaAfter.Status == TaskStatusCompleted {
		t.Fatalf("unmet completion criteria must not complete task: %#v", criteriaAfter)
	}

	retryTask, err := rt.CreateTask(ctx, CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "retry",
		Type:         TaskTypeWorker,
		OwnerAgentID: "agent-a",
		RetryPolicy:  RetryPolicy{MaxAttempts: 2},
	})
	if err != nil {
		t.Fatalf("CreateTask(retry) error = %v", err)
	}
	retryLease := leaseTask(t, ctx, rt, run.ID, retryTask.ID, HolderAgent, "agent-a")
	if err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
		RunID:       run.ID,
		TaskID:      retryTask.ID,
		LeaseID:     retryLease.ID,
		HolderType:  HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: retryTask.Version,
		Report:      TypedReport{Status: ReportStatusFailed, Summary: "transient"},
	}); err != nil {
		t.Fatalf("SubmitTypedReport(failed retry) error = %v", err)
	}
	retryAfter, err := rt.Task(ctx, run.ID, retryTask.ID)
	if err != nil {
		t.Fatalf("Task(retry) error = %v", err)
	}
	if retryAfter.Status != TaskStatusDispatched || retryAfter.Attempts != 1 {
		t.Fatalf("failed report should redispatch retryable task, got %#v", retryAfter)
	}

	handoffTask, err := rt.CreateTask(ctx, CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "handoff",
		Type:         TaskTypeWorker,
		OwnerAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("CreateTask(handoff) error = %v", err)
	}
	handoffLease := leaseTask(t, ctx, rt, run.ID, handoffTask.ID, HolderAgent, "agent-a")
	if err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
		RunID:       run.ID,
		TaskID:      handoffTask.ID,
		LeaseID:     handoffLease.ID,
		HolderType:  HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: handoffTask.Version,
		Report: TypedReport{
			Status:  ReportStatusNeedsHandoff,
			Summary: "transfer context",
			Handoff: &HandoffRequest{ToAgentID: "agent-b", Reason: "needs specialist", ContextSummary: "facts collected"},
		},
	}); err != nil {
		t.Fatalf("SubmitTypedReport(needs_handoff) error = %v", err)
	}
	handoffAfter, err := rt.Task(ctx, run.ID, handoffTask.ID)
	if err != nil {
		t.Fatalf("Task(handoff) error = %v", err)
	}
	if handoffAfter.OwnerAgentID != "agent-b" || handoffAfter.Status != TaskStatusDispatched {
		t.Fatalf("needs_handoff should transfer owner and redispatch, got %#v", handoffAfter)
	}

	clarificationTask, err := rt.CreateTask(ctx, CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "clarify",
		Type:         TaskTypeWorker,
		OwnerAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("CreateTask(clarify) error = %v", err)
	}
	clarificationLease := leaseTask(t, ctx, rt, run.ID, clarificationTask.ID, HolderAgent, "agent-a")
	if err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
		RunID:       run.ID,
		TaskID:      clarificationTask.ID,
		LeaseID:     clarificationLease.ID,
		HolderType:  HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: clarificationTask.Version,
		Report:      TypedReport{Status: ReportStatusNeedsClarification, Summary: "which region?"},
	}); err != nil {
		t.Fatalf("SubmitTypedReport(needs_clarification) error = %v", err)
	}
	clarificationRun, err := rt.Run(ctx, run.ID)
	if err != nil {
		t.Fatalf("Run(clarification) error = %v", err)
	}
	if clarificationRun.Status != RunStatusBlocked {
		t.Fatalf("needs_clarification should block run, got %#v", clarificationRun)
	}
	messages := rt.ResponseOutbox(run.ID)
	if len(messages) == 0 || messages[len(messages)-1].Type != UserMessageTypeClarificationRequest {
		t.Fatalf("needs_clarification should queue clarification request, got %#v", messages)
	}
}

func TestReplayRunStateRebuildsFromEventsAndResponsePublishIsIdempotent(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run, _, err := rt.StartRun(ctx, StartRunCommand{RunID: "run-replay", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := rt.CreateTask(ctx, CreateTaskCommand{
		RunID:          run.ID,
		TaskID:         "response",
		Type:           TaskTypeResponse,
		OwnerComponent: "response_composer",
	})
	if err != nil {
		t.Fatalf("CreateTask(response) error = %v", err)
	}
	lease := leaseTask(t, ctx, rt, run.ID, task.ID, HolderComponent, "response_composer")
	if err := rt.SubmitResponseOutput(ctx, SubmitResponseOutputCommand{
		RunID:       run.ID,
		TaskID:      task.ID,
		LeaseID:     lease.ID,
		HolderType:  HolderComponent,
		HolderID:    "response_composer",
		TaskVersion: task.Version,
		Type:        UserMessageTypeFinalAnswer,
		Title:       "Answer",
		Payload:     "final answer",
	}); err != nil {
		t.Fatalf("SubmitResponseOutput() error = %v", err)
	}
	message := rt.ResponseOutbox(run.ID)[0]
	if err := rt.PublishResponse(ctx, PublishResponseCommand{RunID: run.ID, MessageID: message.ID}); err != nil {
		t.Fatalf("PublishResponse() error = %v", err)
	}
	if err := rt.PublishResponse(ctx, PublishResponseCommand{RunID: run.ID, MessageID: message.ID}); err != nil {
		t.Fatalf("PublishResponse() should be idempotent, got %v", err)
	}

	rt.runs[run.ID] = Run{ID: run.ID, Status: RunStatusFailed}
	rt.tasks[run.ID][task.ID] = Task{ID: task.ID, RunID: run.ID, Status: TaskStatusFailed}
	rt.messages[message.ID] = UserMessage{ID: message.ID, RunID: run.ID, Status: UserMessageFailed}

	projection, err := rt.ReplayRunState(run.ID)
	if err != nil {
		t.Fatalf("ReplayRunState() error = %v", err)
	}
	if projection.Run.Status == RunStatusFailed {
		t.Fatalf("ReplayRunState must use EventStore, not corrupted snapshots: %#v", projection.Run)
	}
	if projection.Tasks[task.ID].Status != TaskStatusCompleted {
		t.Fatalf("expected replayed completed response task, got %#v", projection.Tasks[task.ID])
	}
	if len(projection.Messages) != 1 || projection.Messages[0].Status != UserMessagePublished {
		t.Fatalf("expected replayed published user message, got %#v", projection.Messages)
	}
	audit, err := rt.Replay(run.ID, ReplayModeAudit)
	if err != nil {
		t.Fatalf("Replay(audit) error = %v", err)
	}
	if audit.SideEffects.MailboxDeliveries != 0 || audit.SideEffects.UserMessagePublications != 0 || audit.SideEffects.ActionExecutions != 0 {
		t.Fatalf("audit replay performed side effects: %#v", audit.SideEffects)
	}
}

func TestExpiredLeaseLateReportAndTerminalRunCommandsAreRejected(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run, _, err := rt.StartRun(ctx, StartRunCommand{RunID: "run-late", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := rt.CreateTask(ctx, CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "worker",
		Type:         TaskTypeWorker,
		OwnerAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("CreateTask(worker) error = %v", err)
	}
	env, err := rt.DispatchTask(ctx, DispatchTaskCommand{RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a"})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	lease, acquired, err := rt.AcquireTaskExecution(ctx, AcquireTaskExecutionCommand{
		RunID:      run.ID,
		TaskID:     task.ID,
		EnvelopeID: env.ID,
		HolderType: HolderAgent,
		HolderID:   "agent-a",
		TTL:        time.Nanosecond,
	})
	if err != nil || !acquired {
		t.Fatalf("AcquireTaskExecution() lease=%#v acquired=%v err=%v", lease, acquired, err)
	}
	time.Sleep(time.Millisecond)
	if err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
		RunID:       run.ID,
		TaskID:      task.ID,
		LeaseID:     lease.ID,
		HolderType:  HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: task.Version,
		Report:      TypedReport{Status: ReportStatusSuccess, Summary: "late"},
	}); !errors.Is(err, ErrLeaseNotActive) {
		t.Fatalf("expected expired lease late report rejection, got %v", err)
	}
	if err := rt.TransitionRun(ctx, TransitionRunCommand{RunID: run.ID, To: RunStatusCancelled}); err != nil {
		t.Fatalf("TransitionRun(cancelled) error = %v", err)
	}
	if err := rt.SubmitUserInput(ctx, SubmitUserInputCommand{RunID: run.ID, TaskID: task.ID, Input: "late"}); !errors.Is(err, ErrTerminalState) {
		t.Fatalf("expected terminal run user input rejection, got %v", err)
	}
}
