package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Viking602/venat"
	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/hook"
	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/provider/scripted"
	"github.com/Viking602/venat/skill"
	"github.com/Viking602/venat/tool"
)

func TestAgentWorkerExecutesEnvelope(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	runner.RegisterAgent(api.AgentProfile{ID: "agent-a"})
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{RunID: "run-worker", RootTaskID: "root", Request: "do work"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:         run.ID,
		TaskID:        "task-worker",
		Goal:          "summarize",
		OwnerAgentID:  "agent-a",
		WriteTargets:  []string{"summary"},
		ReadSelectors: []api.BlackboardSelector{{Keys: []string{"input"}}},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if err := runner.WriteItem(ctx, api.BlackboardItem{
		RunID:      run.ID,
		Type:       api.BlackboardItemContext,
		Source:     api.SourceIdentity{Type: api.SourceSystem, ID: "test"},
		Visibility: api.BlackboardVisibilityAgentVisible,
		Key:        "input",
		Payload:    "source material",
	}); err != nil {
		t.Fatalf("WriteItem() error = %v", err)
	}
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a"})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	engine := agent.Engine{Provider: scripted.New([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "summary done"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	})}
	if _, err := (AgentWorker{Runner: runner, Engine: engine, AgentID: "agent-a", Model: "scripted"}).ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{Envelope: env}); err != nil {
		t.Fatalf("ExecuteEnvelope() error = %v", err)
	}
	completed, err := runner.Task(ctx, run.ID, task.ID)
	if err != nil {
		t.Fatalf("Task() error = %v", err)
	}
	if completed.Status != api.TaskStatusCompleted {
		t.Fatalf("expected completed task, got %#v", completed.Status)
	}
	items, err := runner.SelectItems(ctx, run.ID, api.BlackboardSelector{Keys: []string{"summary"}})
	if err != nil {
		t.Fatalf("SelectItems() error = %v", err)
	}
	if len(items) != 1 || items[0].Payload != "summary done" {
		t.Fatalf("missing worker output item: %#v", items)
	}
}

func TestAgentWorkerContinuesFromCompletedTurnCheckpointWithoutReplayingProvider(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	runner.RegisterAgent(api.AgentProfile{ID: "agent-a"})
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{
		RunID: "run-completed-turn-crash", RootTaskID: "root", Request: "finish once",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID: run.ID, TaskID: "task-completed-turn-crash", Goal: "finish once",
		OwnerAgentID: "agent-a", WriteTargets: []string{"summary"},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	envelope, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	checkpoint, err := agent.NewExecutionCheckpointedEvent(agent.ExecutionCheckpointRecord{
		RunID: run.ID, TaskID: task.ID, AgentID: "agent-a", ExecutionID: "execution-before-crash",
		Checkpoint: agent.TurnCheckpoint{
			Messages: []message.Message{message.NewText(message.RoleAssistant, "completed before crash")},
			Usage:    provider.Usage{InputTokens: 7, OutputTokens: 3, TotalTokens: 10},
			Step: agent.Step{
				Index: 0, Decision: agent.StepDecisionFinish,
				ModelCall: &agent.ModelCall{Model: "scripted", StopReason: provider.StopReasonComplete},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewExecutionCheckpointedEvent() error = %v", err)
	}
	if err := runner.AppendEvent(ctx, checkpoint); err != nil {
		t.Fatalf("AppendEvent(checkpoint) error = %v", err)
	}
	providerDriver := &recordingProvider{events: []provider.Event{
		{Kind: provider.EventError, Err: errors.New("provider must not be replayed")},
	}}
	outcome, err := (AgentWorker{
		Runner: runner, Engine: agent.Engine{Provider: providerDriver}, AgentID: "agent-a", Model: "scripted",
	}).ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{Envelope: envelope})
	if err != nil || outcome.State != ExecutionCompleted {
		t.Fatalf("ExecuteEnvelope() outcome=%#v error=%v, want completed", outcome, err)
	}
	if len(providerDriver.requests) != 0 {
		t.Fatalf("provider was replayed after completed-turn checkpoint: %d requests", len(providerDriver.requests))
	}
	if outcome.Result.Text != "completed before crash" || outcome.Result.Usage.TotalTokens != 10 {
		t.Fatalf("recovered terminal result = %#v", outcome.Result)
	}
}

func TestAgentWorkerRestoresStructuredOutputFromTerminalCheckpoint(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	runner.RegisterAgent(api.AgentProfile{ID: "agent-a"})
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{
		RunID: "run-structured-checkpoint", RootTaskID: "root", Request: "finish once",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID: run.ID, TaskID: "task-structured-checkpoint", Goal: "finish once",
		OwnerAgentID: "agent-a", WriteTargets: []string{"summary"},
		OutputSchema: json.RawMessage(`{"type":"object","required":["summary"],"properties":{"summary":{"type":"string"}}}`),
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	envelope, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	checkpoint, err := agent.NewExecutionCheckpointedEvent(agent.ExecutionCheckpointRecord{
		RunID: run.ID, TaskID: task.ID, AgentID: "agent-a", ExecutionID: "execution-before-crash",
		Checkpoint: agent.TurnCheckpoint{
			Messages: []message.Message{message.NewText(message.RoleAssistant, `{"summary":"completed before crash"}`)},
			Step: agent.Step{
				Index: 0, Decision: agent.StepDecisionFinish,
				ModelCall: &agent.ModelCall{Model: "scripted", StopReason: provider.StopReasonComplete},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewExecutionCheckpointedEvent() error = %v", err)
	}
	if err := runner.AppendEvent(ctx, checkpoint); err != nil {
		t.Fatalf("AppendEvent(checkpoint) error = %v", err)
	}
	providerDriver := &recordingProvider{events: []provider.Event{
		{Kind: provider.EventError, Err: errors.New("provider must not be replayed")},
	}}
	outcome, err := (AgentWorker{
		Runner: runner, Engine: agent.Engine{Provider: providerDriver}, AgentID: "agent-a", Model: "scripted",
	}).ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{Envelope: envelope})
	if err != nil || outcome.State != ExecutionCompleted {
		t.Fatalf("ExecuteEnvelope() outcome=%#v error=%v, want completed", outcome, err)
	}
	if len(providerDriver.requests) != 0 {
		t.Fatalf("provider was replayed after valid structured checkpoint: %d requests", len(providerDriver.requests))
	}
	if !outcome.Result.Valid || string(outcome.Result.Structured) != `{"summary":"completed before crash"}` {
		t.Fatalf("recovered structured result = %#v", outcome.Result)
	}
	completed, err := runner.Task(ctx, run.ID, task.ID)
	if err != nil {
		t.Fatalf("Task() error = %v", err)
	}
	if completed.Result == nil || completed.Result.Structured["summary"] != "completed before crash" {
		t.Fatalf("persisted structured report = %#v", completed.Result)
	}
}

func TestAppendTaskExecutionEventRejectsReleasedLease(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	runner.RegisterAgent(api.AgentProfile{ID: "agent-a"})
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{RunID: "run-checkpoint-fence", RootTaskID: "root"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID: run.ID, TaskID: "task-checkpoint-fence", OwnerAgentID: "agent-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, acquired, err := runner.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{
		RunID: run.ID, TaskID: task.ID, EnvelopeID: envelope.ID,
		HolderType: api.HolderAgent, HolderID: "agent-a", TTL: time.Minute,
	})
	if err != nil || !acquired {
		t.Fatalf("AcquireTaskExecution() lease=%#v acquired=%v error=%v", lease, acquired, err)
	}
	event, err := agent.NewExecutionCheckpointedEvent(agent.ExecutionCheckpointRecord{
		RunID: run.ID, TaskID: task.ID, AgentID: "agent-a", ExecutionID: lease.ID,
		Checkpoint: agent.TurnCheckpoint{Messages: []message.Message{message.NewText(message.RoleAssistant, "stale")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	oversized := event
	oversized.Payload = map[string]any{"record": strings.Repeat("x", 8<<20)}
	err = runner.AppendTaskExecutionEvent(ctx, api.AppendTaskExecutionEventCommand{
		RunID: run.ID, TaskID: task.ID, LeaseID: lease.ID,
		HolderType: api.HolderAgent, HolderID: "agent-a",
		TaskVersion: task.Version, Event: oversized,
	})
	if !errors.Is(err, api.ErrCheckpointLimitExceeded) {
		t.Fatalf("oversized AppendTaskExecutionEvent() error=%v, want ErrCheckpointLimitExceeded", err)
	}
	if err := runner.ReleaseTaskExecution(ctx, api.ReleaseTaskExecutionCommand{LeaseID: lease.ID, HolderID: "agent-a"}); err != nil {
		t.Fatal(err)
	}
	err = runner.AppendTaskExecutionEvent(ctx, api.AppendTaskExecutionEventCommand{
		RunID: run.ID, TaskID: task.ID, LeaseID: lease.ID,
		HolderType: api.HolderAgent, HolderID: "agent-a",
		TaskVersion: task.Version, Event: event,
	})
	if !errors.Is(err, api.ErrLeaseNotActive) {
		t.Fatalf("AppendTaskExecutionEvent() error=%v, want ErrLeaseNotActive", err)
	}
	events, err := runner.ListEvents(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, stored := range events {
		if stored.Type == api.EventExecutionCheckpointed {
			t.Fatalf("released worker appended checkpoint: %#v", stored)
		}
	}
}

func TestAgentWorkerRejectsUnpersistedSuppliedLeaseBeforeProviderCall(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	runner.RegisterAgent(api.AgentProfile{ID: "agent-a"})
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{RunID: "run-unpersisted-lease", RootTaskID: "root"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID: run.ID, TaskID: "task-unpersisted-lease", OwnerAgentID: "agent-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	driver := &recordingProvider{events: []provider.Event{{Kind: provider.EventDone}}}
	_, err = (AgentWorker{
		Runner: runner, Engine: agent.Engine{Provider: driver}, AgentID: "agent-a", Model: "scripted",
	}).ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{
		Envelope: envelope,
		Lease: api.TaskExecutionLease{
			ID: "missing-lease", RunID: run.ID, TaskID: task.ID,
			HolderType: api.HolderAgent, HolderID: "agent-a", Status: api.LeaseStatusActive,
		},
	})
	if !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("ExecuteEnvelope() error=%v, want ErrNotFound", err)
	}
	if len(driver.requests) != 0 {
		t.Fatalf("provider calls=%d, want zero before lease validation", len(driver.requests))
	}
}

func TestAgentWorkerPersistsStepTrace(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	runner.RegisterAgent(api.AgentProfile{ID: "agent-a"})
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{
		RunID:      "run-worker-step-trace",
		RootTaskID: "root",
		Request:    "do work",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "task-worker-step-trace",
		Goal:         "summarize",
		OwnerAgentID: "agent-a",
		WriteTargets: []string{"summary"},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	envelope, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
		RunID:         run.ID,
		TaskID:        task.ID,
		TargetAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}

	callerRecorded := false
	engine := agent.Engine{
		Provider: scripted.New([]provider.Event{
			{Kind: provider.EventTextDelta, Text: "summary done"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}),
		StepRecorder: agent.StepRecorderFunc(func(ctx context.Context, step agent.Step) error {
			events, err := runner.ListEvents(ctx, run.ID)
			if err != nil {
				return fmt.Errorf("list durable events from caller recorder: %w", err)
			}
			records, err := agent.ReconstructStepTrace(events, agent.StepSelector{
				RunID:   run.ID,
				TaskID:  task.ID,
				AgentID: "agent-a",
			})
			if err != nil {
				return fmt.Errorf("reconstruct durable trace from caller recorder: %w", err)
			}
			if len(records) != 1 || records[0].Step.Index != step.Index || records[0].Step.Decision != step.Decision {
				return fmt.Errorf("durable recorder did not run before caller recorder: records=%#v step=%#v", records, step)
			}
			callerRecorded = true
			return nil
		}),
	}
	if _, err := (AgentWorker{
		Runner:  runner,
		Engine:  engine,
		AgentID: "agent-a",
		Model:   "scripted",
	}).ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{Envelope: envelope}); err != nil {
		t.Fatalf("ExecuteEnvelope() error = %v", err)
	}
	if !callerRecorded {
		t.Fatal("caller-supplied StepRecorder was not invoked")
	}

	events, err := runner.ListEvents(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	records, err := agent.ReconstructStepTrace(events, agent.StepSelector{
		RunID:   run.ID,
		TaskID:  task.ID,
		AgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("ReconstructStepTrace() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("step records = %#v, want one finalized step", records)
	}
	record := records[0]
	if record.RunID != run.ID || record.TaskID != task.ID || record.AgentID != "agent-a" {
		t.Fatalf("step record identity = %#v, want run/task/agent binding", record)
	}
	if record.ExecutionID == "" {
		t.Fatal("step record execution ID is blank")
	}
	if record.Step.Index != 0 || record.Step.Decision != agent.StepDecisionFinish {
		t.Fatalf("persisted step = %#v, want finalized step 0", record.Step)
	}
	var acquiredLeaseID string
	for _, event := range events {
		if event.Type == api.EventTaskExecutionAcquired && event.TaskID == task.ID {
			acquiredLeaseID, _ = event.Payload["leaseId"].(string)
			break
		}
	}
	if acquiredLeaseID == "" || record.ExecutionID != acquiredLeaseID {
		t.Fatalf("step execution ID = %q, acquired lease ID = %q", record.ExecutionID, acquiredLeaseID)
	}
	selected, err := agent.ReconstructStepTrace(events, agent.StepSelector{ExecutionID: record.ExecutionID})
	if err != nil {
		t.Fatalf("ReconstructStepTrace(execution) error = %v", err)
	}
	if len(selected) != 1 || selected[0].ExecutionID != record.ExecutionID {
		t.Fatalf("execution-selected records = %#v, want persisted execution", selected)
	}
}

func TestAgentWorkerStepPersistenceFailureFailsTask(t *testing.T) {
	ctx := context.Background()
	stepAppendErr := errors.New("step event append failed")
	backing := venat.NewDevelopment()
	runner := venat.NewDevelopment(api.Config{
		StoreProvider: stepEventFailingProvider{
			StoreProvider: backing,
			err:           stepAppendErr,
		},
	})
	runner.RegisterAgent(api.AgentProfile{ID: "agent-a"})
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{
		RunID:      "run-worker-step-failure",
		RootTaskID: "root",
		Request:    "do work",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "task-worker-step-failure",
		Goal:         "summarize",
		OwnerAgentID: "agent-a",
		WriteTargets: []string{"summary"},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	envelope, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
		RunID:         run.ID,
		TaskID:        task.ID,
		TargetAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}

	callerRecorded := false
	_, err = (AgentWorker{
		Runner: runner,
		Engine: agent.Engine{
			Provider: scripted.New([]provider.Event{
				{Kind: provider.EventTextDelta, Text: "summary done"},
				{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
			}),
			StepRecorder: agent.StepRecorderFunc(func(context.Context, agent.Step) error {
				callerRecorded = true
				return nil
			}),
		},
		AgentID: "agent-a",
		Model:   "scripted",
	}).ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{Envelope: envelope})
	if !errors.Is(err, stepAppendErr) {
		t.Fatalf("ExecuteEnvelope() error = %v, want %v", err, stepAppendErr)
	}
	if callerRecorded {
		t.Fatal("caller StepRecorder ran after durable recorder failed")
	}

	failed, err := runner.Task(ctx, run.ID, task.ID)
	if err != nil {
		t.Fatalf("Task() error = %v", err)
	}
	if failed.Status != api.TaskStatusFailed || failed.Result == nil {
		t.Fatalf("task after step persistence failure = %#v, want failed report", failed)
	}
	if failed.Result.Status != api.ReportStatusFailed || failed.Result.Kind != string(agent.FailureKindEngineError) {
		t.Fatalf("failure report = %#v, want engine failure", failed.Result)
	}
	events, err := runner.ListEvents(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	records, err := agent.ReconstructStepTrace(events, agent.StepSelector{
		RunID:       run.ID,
		TaskID:      task.ID,
		AgentID:     "agent-a",
		ExecutionID: "",
	})
	if err != nil {
		t.Fatalf("ReconstructStepTrace() error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("persisted step records = %#v, want none after rejected append", records)
	}
	for _, event := range events {
		if event.Type == api.EventTaskCompleted && event.TaskID == task.ID {
			t.Fatalf("success completion event was submitted after recorder failure: %#v", event)
		}
	}
}

func TestAgentWorkerInjectsEngineSkillsIntoWorkerContext(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	runner.RegisterAgent(api.AgentProfile{ID: "agent-a"})
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{RunID: "run-worker-skills", RootTaskID: "root", Request: "do work"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "task-worker-skills",
		Goal:         "summarize skills",
		OwnerAgentID: "agent-a",
		WriteTargets: []string{"summary"},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a"})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	recorder := &recordingProvider{events: []provider.Event{
		{Kind: provider.EventTextDelta, Text: "done"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}}
	engine := agent.Engine{
		Provider: recorder,
		Skills: []skill.Skill{{
			Name:        "worker-skill",
			Description: "worker context",
			Body:        "worker body",
		}},
	}

	if _, err := (AgentWorker{Runner: runner, Engine: engine, AgentID: "agent-a", Model: "scripted"}).ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{Envelope: env}); err != nil {
		t.Fatalf("ExecuteEnvelope() error = %v", err)
	}
	if len(recorder.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(recorder.requests))
	}
	got := recorder.requests[0].Messages
	if len(got) != 3 {
		t.Fatalf("provider messages = %d, want worker system + skills + user: %+v", len(got), got)
	}
	if got[0].Role != message.RoleSystem || got[0].Text != "You are Venat agent agent-a. Complete the assigned task and return a concise result." {
		t.Fatalf("first message = %+v, want worker system identity", got[0])
	}
	if got[1].Role != message.RoleSystem ||
		!strings.Contains(got[1].Text, "--- skill: worker-skill ---") ||
		!strings.Contains(got[1].Text, "worker body") {
		t.Fatalf("second message = %+v, want worker skill system section", got[1])
	}
	if got[2].Role != message.RoleUser || got[2].Text != "summarize skills" {
		t.Fatalf("third message = %+v, want task goal prompt", got[2])
	}
}

func TestAgentWorkerValidatesAgainstTaskOutputSchema(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	runner.RegisterAgent(api.AgentProfile{ID: "agent-a"})
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{RunID: "run-schema", RootTaskID: "root", Request: "do work"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "task-schema",
		Goal:         "summarize",
		OwnerAgentID: "agent-a",
		WriteTargets: []string{"summary"},
		OutputSchema: json.RawMessage(`{"type":"object","required":["summary"],"properties":{"summary":{"type":"string"}}}`),
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	// The task's OutputSchema must survive the public CreateTask path so the
	// worker can validate against it; an empty schema here would make the test
	// vacuous (validation would be skipped).
	if len(task.OutputSchema) == 0 {
		t.Fatalf("CreateTask dropped OutputSchema: %#v", task)
	}
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a"})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	// The model returns prose, not JSON — invalid against the task's OutputSchema.
	engine := agent.Engine{Provider: scripted.New([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "just some prose, not json"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	})}
	_, err = (AgentWorker{Runner: runner, Engine: engine, AgentID: "agent-a", Model: "scripted"}).ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{Envelope: env})
	if err == nil {
		t.Fatal("expected schema-validation failure on worker path, got nil error")
	}
	failed, err := runner.Task(ctx, run.ID, task.ID)
	if err != nil {
		t.Fatalf("Task() error = %v", err)
	}
	if failed.Status != api.TaskStatusFailed {
		t.Fatalf("schema-invalid terminal output must fail the task, got status %#v", failed.Status)
	}
}

func TestAgentWorkerPersistsValidatedStructuredOutput(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	runner.RegisterAgent(api.AgentProfile{ID: "agent-a"})
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{RunID: "run-structured", RootTaskID: "root", Request: "do work"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "task-structured",
		Goal:         "summarize",
		OwnerAgentID: "agent-a",
		WriteTargets: []string{"summary"},
		OutputSchema: json.RawMessage(`{"type":"object","required":["summary"],"properties":{"summary":{"type":"string"}}}`),
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a"})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	// The model returns valid JSON that satisfies the task's OutputSchema, so
	// the engine populates result.Structured.
	engine := agent.Engine{Provider: scripted.New([]provider.Event{
		{Kind: provider.EventTextDelta, Text: `{"summary":"done"}`},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	})}
	if _, err := (AgentWorker{Runner: runner, Engine: engine, AgentID: "agent-a", Model: "scripted"}).ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{Envelope: env}); err != nil {
		t.Fatalf("ExecuteEnvelope() error = %v", err)
	}
	completed, err := runner.Task(ctx, run.ID, task.ID)
	if err != nil {
		t.Fatalf("Task() error = %v", err)
	}
	if completed.Status != api.TaskStatusCompleted {
		t.Fatalf("expected completed task, got %#v", completed.Status)
	}
	// The validated structured payload must survive onto the persisted report;
	// a success report carrying only Summary would drop it, leaving durable
	// downstream readers (routers, graph edges) unable to route on it.
	if completed.Result == nil {
		t.Fatalf("completed task carries no typed report")
	}
	if completed.Result.Structured == nil || completed.Result.Structured["summary"] != "done" {
		t.Fatalf("validated structured output dropped from report: %#v", completed.Result)
	}
}

func TestGovernedToolBusRejectsSideEffectWithoutActionTask(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{RunID: "run-tool", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{RunID: run.ID, TaskID: "tool-task", OwnerAgentID: "agent-a"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a"})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	lease, _, err := runner.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{
		RunID: run.ID, TaskID: task.ID, EnvelopeID: env.ID, HolderType: api.HolderAgent, HolderID: "agent-a", TTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("AcquireTaskExecution() error = %v", err)
	}
	driver := &recordingTool{definition: tool.Definition{Name: "write", EffectType: tool.EffectWrite, RequiresActionTask: true}}
	bus := GovernedToolBus{
		Runner: runner, Bus: tool.NewBus(driver), RunID: run.ID, TaskID: task.ID,
		LeaseID: lease.ID, HolderType: api.HolderAgent, HolderID: "agent-a", TaskVersion: task.Version,
	}
	_, err = bus.Execute(ctx, tool.Call{ID: "call-1", Name: "write", Arguments: json.RawMessage(`{"value":1}`)}, nil)
	if !errors.Is(err, venat.ErrActionTaskRequired) {
		t.Fatalf("expected ErrActionTaskRequired, got %v", err)
	}
	if driver.called {
		t.Fatalf("side-effect driver was called despite governance rejection")
	}
}

func TestGovernedToolBusPreparesApprovalBeforeStartingActionAttempt(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{RunID: "run-action-preflight", RootTaskID: "root"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID: run.ID, TaskID: "action", OwnerAgentID: "agent-a", AllowsAction: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, acquired, err := runner.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{
		RunID: run.ID, TaskID: task.ID, EnvelopeID: envelope.ID,
		HolderType: api.HolderAgent, HolderID: "agent-a", TTL: time.Minute,
	})
	if err != nil || !acquired {
		t.Fatalf("AcquireTaskExecution() lease=%#v acquired=%v error=%v", lease, acquired, err)
	}
	driver := &approvalPreparingTool{}
	bus := GovernedToolBus{
		Runner: runner, Bus: tool.NewBus(driver), RunID: run.ID, TaskID: task.ID,
		LeaseID: lease.ID, HolderType: api.HolderAgent, HolderID: "agent-a", TaskVersion: task.Version,
	}
	_, err = bus.Execute(ctx, tool.Call{ID: "call", Name: "write", Arguments: json.RawMessage(`{}`)}, nil)
	if !errors.Is(err, errPreparingApproval) {
		t.Fatalf("Execute() error=%v, want preparation approval error", err)
	}
	attempts, listErr := runner.ListActionAttempts(ctx, api.ActionAttemptSelector{RunID: run.ID, TaskID: task.ID})
	if listErr != nil || len(attempts) != 0 || driver.executed {
		t.Fatalf("preflight attempts=%#v executed=%v error=%v", attempts, driver.executed, listErr)
	}
}

func TestGovernedToolBusPersistsActionAttempt(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{RunID: "run-action-tool", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "action-task",
		OwnerAgentID: "agent-a",
		AllowsAction: true,
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a"})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	lease, acquired, err := runner.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{
		RunID: run.ID, TaskID: task.ID, EnvelopeID: env.ID, HolderType: api.HolderAgent, HolderID: "agent-a", TTL: time.Minute,
	})
	if err != nil || !acquired {
		t.Fatalf("AcquireTaskExecution() lease=%#v acquired=%v err=%v", lease, acquired, err)
	}
	driver := &recordingTool{definition: tool.Definition{Name: "write", EffectType: tool.EffectWrite, RequiresActionTask: true}}
	bus := GovernedToolBus{
		Runner: runner, Bus: tool.NewBus(driver), RunID: run.ID, TaskID: task.ID,
		LeaseID: lease.ID, HolderType: api.HolderAgent, HolderID: "agent-a", TaskVersion: task.Version,
	}
	call := tool.Call{ID: "call-1", OperationID: "turn:2:call:0", Name: "write", Arguments: json.RawMessage(`{"value":1,"meta":{"b":2,"a":1}}`)}
	if _, err := bus.Execute(ctx, call, nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	attempts, err := runner.ListActionAttempts(ctx, api.ActionAttemptSelector{
		RunID: run.ID, TaskID: task.ID, ToolName: call.Name,
	})
	if err != nil || len(attempts) != 1 {
		t.Fatalf("ListActionAttempts() attempts=%#v error=%v", attempts, err)
	}
	attempt := attempts[0]
	if attempt.Status != api.ActionAttemptSucceeded || attempt.ExternalResultRef != "ok" ||
		!strings.Contains(string(attempt.ToolResult), `"structured":{"receipt":"stored"}`) {
		t.Fatalf("action attempt = %#v, want succeeded with exact output", attempt)
	}
	cached, err := bus.Execute(ctx, tool.Call{ID: "call-regenerated", OperationID: call.OperationID, Name: call.Name, Arguments: json.RawMessage(`{"meta":{"a":1,"b":2},"value":1}`)}, nil)
	if err != nil {
		t.Fatalf("regenerated completed Execute() error = %v", err)
	}
	if cached.IsError || cached.ToolCallID != "call-regenerated" || cached.Content != "ok" ||
		string(cached.Structured) != `{"receipt":"stored"}` || driver.calls != 1 {
		t.Fatalf("regenerated completed Execute() = %#v, driver calls=%d; want exact cached success without replay", cached, driver.calls)
	}
	_, err = bus.Execute(ctx, tool.Call{
		ID: "call-changed", OperationID: call.OperationID, Name: call.Name, Arguments: json.RawMessage(`{"value":9}`),
	}, nil)
	if !errors.Is(err, api.ErrIdempotencyConflict) || driver.calls != 1 {
		t.Fatalf("changed operation slot error=%v driver calls=%d, want idempotency conflict without replay", err, driver.calls)
	}
	repeated, err := bus.Execute(ctx, tool.Call{
		ID: "call-repeat", OperationID: "turn:4:call:0", Name: call.Name, Arguments: call.Arguments,
	}, nil)
	if err != nil || repeated.IsError || driver.calls != 2 {
		t.Fatalf("legitimate repeated Execute()=%#v error=%v driver calls=%d, want second execution", repeated, err, driver.calls)
	}
	driver.err = errors.Join(tool.ErrNotExecuted, context.Canceled)
	_, err = bus.Execute(ctx, tool.Call{
		ID: "call-not-executed", OperationID: "turn:6:call:0", Name: call.Name, Arguments: json.RawMessage(`{"value":2}`),
	}, nil)
	if !errors.Is(err, tool.ErrNotExecuted) {
		t.Fatalf("not-executed Execute() error=%v", err)
	}
	attempts, err = runner.ListActionAttempts(ctx, api.ActionAttemptSelector{
		RunID: run.ID, TaskID: task.ID, ToolName: call.Name,
	})
	if err != nil || len(attempts) != 3 {
		t.Fatalf("ListActionAttempts() attempts=%#v error=%v, want three logical operation slots", attempts, err)
	}
	var notExecuted api.ActionAttempt
	for _, candidate := range attempts {
		if candidate.Status == api.ActionAttemptFailed {
			notExecuted = candidate
		}
	}
	if notExecuted.AttemptID == "" || notExecuted.RequiresReconcile {
		t.Fatalf("not-executed action attempts=%#v, want failed without reconciliation", attempts)
	}
}

func TestAgentWorkerResumesReconciledActionFromCheckpoint(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	runner.RegisterAgent(api.AgentProfile{ID: "agent-a"})
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{
		RunID:      "run-reconcile-resume",
		RootTaskID: "root",
		Request:    "perform the write",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "action-task",
		Goal:         "write once",
		OwnerAgentID: "agent-a",
		AllowsAction: true,
		WriteTargets: []string{"result"},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	envelope, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
		RunID:         run.ID,
		TaskID:        task.ID,
		TargetAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	providerDriver := &sequenceProvider{turns: [][]provider.Event{
		{
			{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{
				ID: "call-1", Name: "write", Arguments: json.RawMessage(`{"value":1}`),
			}},
			{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
		},
		{
			{Kind: provider.EventTextDelta, Text: "write confirmed"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		},
	}}
	driverErr := errors.New("connection lost after write")
	actionDriver := &recordingTool{
		definition: tool.Definition{
			Name:               "write",
			EffectType:         tool.EffectExternalSideEffect,
			RequiresActionTask: true,
		},
		err: driverErr,
	}
	worker := AgentWorker{
		Runner:  runner,
		Engine:  agent.Engine{Provider: providerDriver, Tools: tool.NewBus(actionDriver)},
		AgentID: "agent-a",
		Model:   "scripted",
	}

	first, err := worker.ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{Envelope: envelope})
	if first.State != ExecutionSuspended || first.Suspension == nil ||
		first.Suspension.Kind != SuspensionReconciliation {
		t.Fatalf("first ExecuteEnvelope() state=%q suspension=%#v error=%v", first.State, first.Suspension, err)
	}
	attempts, err := runner.ListActionAttempts(ctx, api.ActionAttemptSelector{
		RunID: run.ID, TaskID: task.ID,
	})
	if err != nil || len(attempts) != 1 {
		t.Fatalf("ListActionAttempts() attempts=%#v error=%v", attempts, err)
	}
	if _, err := runner.ResolveActionAttempt(ctx, api.ResolveActionAttemptCommand{
		AttemptID:         attempts[0].AttemptID,
		Status:            api.ActionAttemptSucceeded,
		ExternalResultRef: "receipt-1",
	}); err != nil {
		t.Fatalf("ResolveActionAttempt() error = %v", err)
	}

	envelopes, err := runner.ListEnvelopes(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListEnvelopes() after reconciliation error = %v", err)
	}
	var resumedEnvelope api.TaskEnvelope
	for _, candidate := range envelopes {
		if candidate.TaskID == task.ID && candidate.Status == "pending" {
			resumedEnvelope = candidate
			break
		}
	}
	if resumedEnvelope.ID == "" {
		t.Fatalf("reconciliation did not atomically queue a resume envelope: %#v", envelopes)
	}
	envelope = resumedEnvelope
	second, err := worker.ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{Envelope: envelope})
	if err != nil || second.State != ExecutionCompleted {
		t.Fatalf("second ExecuteEnvelope() outcome=%#v error=%v, want completed", second, err)
	}
	if actionDriver.calls != 1 {
		t.Fatalf("side-effect driver calls = %d, want exactly one", actionDriver.calls)
	}
	if len(providerDriver.requests) != 2 {
		t.Fatalf("provider requests = %d, want two turns across both executions", len(providerDriver.requests))
	}
	var replayedResult *message.ToolResult
	for _, msg := range providerDriver.requests[1].Messages {
		if msg.Role == message.RoleTool && msg.ToolResult != nil && msg.ToolResult.ToolCallID == "call-1" {
			replayedResult = msg.ToolResult
			break
		}
	}
	if replayedResult == nil || replayedResult.Content != "receipt-1" {
		t.Fatalf("resumed provider messages missing reconciled result: %#v", providerDriver.requests[1].Messages)
	}
}

func TestAgentWorkerResumesApprovedActionFromCheckpoint(t *testing.T) {
	ctx := context.Background()
	policyGate := &approvalPolicy{}
	runner := venat.NewDevelopment(api.Config{PolicyEngine: policyGate})
	runner.RegisterAgent(api.AgentProfile{ID: "agent-a"})
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{
		RunID:      "run-approval-resume",
		RootTaskID: "root",
		Request:    "perform the approved write",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "approval-task",
		Goal:         "write once after approval",
		OwnerAgentID: "agent-a",
		AllowsAction: true,
		WriteTargets: []string{"result"},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	envelope, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	providerDriver := &sequenceProvider{turns: [][]provider.Event{
		{
			{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{
				ID: "approval-call", Name: "write_alias", Arguments: json.RawMessage(`{"value":1}`),
			}},
			{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
		},
		{
			{Kind: provider.EventTextDelta, Text: "approved write complete"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		},
	}}
	actionDriver := &recordingTool{definition: tool.Definition{
		Name:             "write",
		EffectType:       tool.EffectWrite,
		RequiresApproval: true,
	}}
	toolHook := &resumeToolLifecycleHook{}
	worker := AgentWorker{
		Runner:  runner,
		Engine:  agent.Engine{Provider: providerDriver, Tools: tool.NewBus(actionDriver), Hooks: hook.NewChain(toolHook)},
		AgentID: "agent-a",
		Model:   "scripted",
	}

	first, err := worker.ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{
		Envelope: envelope,
		Messages: []message.Message{message.NewText(message.RoleSystem, "supplemental resume context")},
	})
	if first.State != ExecutionSuspended || first.Suspension == nil ||
		first.Suspension.Kind != SuspensionApproval {
		t.Fatalf("first ExecuteEnvelope() state=%q suspension=%#v error=%v", first.State, first.Suspension, err)
	}
	if actionDriver.calls != 0 {
		t.Fatalf("driver calls before approval = %d, want zero", actionDriver.calls)
	}
	if toolHook.before != 1 || toolHook.after != 0 {
		t.Fatalf("hooks before suspension = before:%d after:%d, want 1 and 0", toolHook.before, toolHook.after)
	}
	attempts, err := runner.ListActionAttempts(ctx, api.ActionAttemptSelector{RunID: run.ID, TaskID: task.ID})
	if err != nil || len(attempts) != 0 {
		t.Fatalf("action attempts before approval=%#v error=%v, want none", attempts, err)
	}
	tokens, err := runner.PendingResumeTokens(ctx, api.ResumeTokenSelector{
		RunID: run.ID, TaskID: task.ID,
	})
	if err != nil || len(tokens) != 1 {
		t.Fatalf("PendingResumeTokens() tokens=%#v error=%v", tokens, err)
	}
	policyGate.approved = true
	if err := runner.DecideApproval(ctx, api.DecideApprovalCommand{
		RunID:      run.ID,
		ApprovalID: tokens[0].ApprovalID,
		DecidedBy:  "reviewer",
		Decision:   "approved",
	}); err != nil {
		t.Fatalf("DecideApproval() error = %v", err)
	}
	envelopes, err := runner.ListEnvelopes(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListEnvelopes() after approval error = %v", err)
	}
	var pending []api.TaskEnvelope
	for _, candidate := range envelopes {
		if candidate.TaskID == task.ID && candidate.Status == "pending" {
			pending = append(pending, candidate)
		}
	}
	if len(pending) != 1 {
		t.Fatalf("approval queued %d resume envelopes, want exactly one: %#v", len(pending), envelopes)
	}
	envelope = pending[0]

	second, err := worker.ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{
		Envelope: envelope,
		Messages: []message.Message{message.NewText(message.RoleSystem, "supplemental resume context")},
	})
	if err != nil || second.State != ExecutionCompleted {
		t.Fatalf("second ExecuteEnvelope() outcome=%#v error=%v, want completed", second, err)
	}
	if actionDriver.calls != 1 {
		t.Fatalf("approved driver calls = %d, want exactly one", actionDriver.calls)
	}
	if len(providerDriver.requests) != 2 {
		t.Fatalf("provider requests = %d, want two turns across both executions", len(providerDriver.requests))
	}
	if toolHook.before != 2 || toolHook.after != 1 {
		t.Fatalf("hooks after resume = before:%d after:%d, want 2 and 1", toolHook.before, toolHook.after)
	}
	var supplementalCount int
	var hookedResult bool
	for _, current := range providerDriver.requests[1].Messages {
		if current.Role == message.RoleSystem && current.Text == "supplemental resume context" {
			supplementalCount++
		}
		if current.Role == message.RoleTool && current.ToolResult != nil &&
			current.ToolResult.ToolCallID == "approval-call" &&
			current.ToolResult.Name == "write" &&
			current.ToolResult.Content == "hooked: ok" {
			hookedResult = true
		}
	}
	if supplementalCount != 1 {
		t.Fatalf("resumed supplemental message count = %d, want 1: %#v", supplementalCount, providerDriver.requests[1].Messages)
	}
	if !hookedResult {
		t.Fatalf("resumed provider messages missing hook-processed result: %#v", providerDriver.requests[1].Messages)
	}
}

func TestAgentWorkerSubmitsFailedReportAndReleasesLeaseOnEngineError(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{RunID: "run-worker-failure", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "task-worker-failure",
		OwnerAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a"})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}

	errBoom := errors.New("provider boom")
	engine := agent.Engine{Provider: failingProvider{err: errBoom}}
	_, err = (AgentWorker{
		Runner:  runner,
		Engine:  engine,
		AgentID: "agent-a",
		Model:   "scripted",
	}).ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{Envelope: env})
	if !errors.Is(err, errBoom) {
		t.Fatalf("ExecuteEnvelope() error = %v, want %v", err, errBoom)
	}

	failed, err := runner.Task(ctx, run.ID, task.ID)
	if err != nil {
		t.Fatalf("Task() error = %v", err)
	}
	if failed.Status != api.TaskStatusFailed || failed.Error != errBoom.Error() {
		t.Fatalf("engine failure should submit failed report, got %#v", failed)
	}
	// The engine wraps the provider error in an AgentFailure; the worker must
	// carry that typed classification onto the persisted report so a scheduler
	// can branch on the failure mode end-to-end.
	if failed.Result == nil || failed.Result.Kind != string(agent.FailureKindEngineError) {
		t.Fatalf("failed report should carry the agent failure kind, got %#v", failed.Result)
	}
	if active := runner.ActiveLeaseCountContext(ctx, run.ID, task.ID); active != 0 {
		t.Fatalf("engine failure should release active lease, got %d", active)
	}
}

func TestFailureReportCarriesAgentFailureDisposition(t *testing.T) {
	failure := (&agent.AgentFailure{
		Kind:        agent.FailureKindBudgetExhausted,
		Reason:      "out of budget",
		Retryable:   true,
		Escalatable: false,
	}).WithCause(errors.New("underlying"))

	// Wrapped so the test also exercises errors.As walking the chain, the way
	// a real caller might re-wrap the failure before it reaches submitFailure.
	report := failureReport(fmt.Errorf("worker: %w", failure))

	if report.Status != api.ReportStatusFailed {
		t.Fatalf("Status = %q, want failed", report.Status)
	}
	if report.Kind != string(agent.FailureKindBudgetExhausted) {
		t.Fatalf("Kind = %q, want budget_exhausted", report.Kind)
	}
	// Distinct true/false guards against the booleans being swapped.
	if !report.Retryable || report.Escalatable {
		t.Fatalf("disposition mismatch: retryable=%v escalatable=%v", report.Retryable, report.Escalatable)
	}
}

func TestFailureReportPlainErrorHasNoKind(t *testing.T) {
	report := failureReport(errors.New("boom"))

	if report.Status != api.ReportStatusFailed || report.Summary != "boom" {
		t.Fatalf("unexpected report: %#v", report)
	}
	if report.Kind != "" || report.Retryable || report.Escalatable {
		t.Fatalf("plain error should leave failure fields empty, got %#v", report)
	}
}

type resumeToolLifecycleHook struct {
	before int
	after  int
}

func (h *resumeToolLifecycleHook) TransformContext(_ context.Context, messages []message.Message) ([]message.Message, error) {
	return messages, nil
}

func (h *resumeToolLifecycleHook) BeforeModelCall(context.Context, *provider.Request) error {
	return nil
}

func (h *resumeToolLifecycleHook) BeforeToolCall(_ context.Context, call *tool.Call) error {
	h.before++
	if call.Name == "write_alias" {
		call.Name = "write"
	}
	return nil
}

func (h *resumeToolLifecycleHook) AfterToolCall(_ context.Context, result *tool.Result) error {
	h.after++
	result.Content = "hooked: " + result.Content
	return nil
}

func (h *resumeToolLifecycleHook) OnEvent(context.Context, provider.Event) error {
	return nil
}

type recordingTool struct {
	definition tool.Definition
	called     bool
	calls      int
	err        error
}

func (d *recordingTool) Definition() tool.Definition { return d.definition }

func (d *recordingTool) Execute(context.Context, tool.Call, tool.UpdateSink) (tool.Result, error) {
	d.called = true
	d.calls++
	if d.err != nil {
		return tool.Result{Name: d.definition.Name}, d.err
	}
	return tool.Result{Name: d.definition.Name, Content: "ok", Structured: json.RawMessage(`{"receipt":"stored"}`)}, nil
}

var errPreparingApproval = errors.New("approval required before execution")

type approvalPreparingTool struct {
	executed bool
}

func (d *approvalPreparingTool) Definition() tool.Definition {
	return tool.Definition{Name: "write", EffectType: tool.EffectWrite, RequiresActionTask: true}
}

func (d *approvalPreparingTool) Prepare(_ context.Context, call tool.Call, _ tool.UpdateSink) (tool.PreparedExecution, error) {
	return tool.PreparedExecution{Call: call}, errPreparingApproval
}

func (d *approvalPreparingTool) Execute(context.Context, tool.Call, tool.UpdateSink) (tool.Result, error) {
	d.executed = true
	return tool.Result{Name: "write"}, nil
}

type approvalPolicy struct {
	approved bool
}

func (p *approvalPolicy) Authorize(_ context.Context, request api.PolicyRequest) (api.PolicyDecision, error) {
	if request.Operation == api.PolicyOperationToolCall && !p.approved {
		return api.PolicyDecision{
			Effect:           api.PolicyEffectRequireApproval,
			Reason:           "review required",
			ApprovalRequired: true,
		}, nil
	}
	return api.PolicyDecision{Effect: api.PolicyEffectAllow}, nil
}

type failingProvider struct {
	err error
}

func (p failingProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "failing"}
}

func (p failingProvider) Stream(context.Context, provider.Request) (provider.Stream, error) {
	return nil, p.err
}

type recordingProvider struct {
	events   []provider.Event
	requests []provider.Request
}

func (p *recordingProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "recording"}
}

func (p *recordingProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	p.requests = append(p.requests, request)
	events := p.events
	if len(events) == 0 {
		events = []provider.Event{{Kind: provider.EventDone, StopReason: provider.StopReasonComplete}}
	}
	return provider.NewSliceStream(events), nil
}

type sequenceProvider struct {
	turns    [][]provider.Event
	requests []provider.Request
	failures map[int]error
}

func (p *sequenceProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "sequence"}
}

func (p *sequenceProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	index := len(p.requests)
	p.requests = append(p.requests, request)
	if err := p.failures[index]; err != nil {
		return nil, err
	}
	if index >= len(p.turns) {
		return nil, fmt.Errorf("unexpected provider request %d", index)
	}
	return provider.NewSliceStream(p.turns[index]), nil
}

type stepEventFailingProvider struct {
	api.StoreProvider
	err error
}

func (p stepEventFailingProvider) Begin(ctx context.Context) (api.UnitOfWork, error) {
	uow, err := p.StoreProvider.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return stepEventFailingUoW{UnitOfWork: uow, err: p.err}, nil
}

type stepEventFailingUoW struct {
	api.UnitOfWork
	err error
}

func (u stepEventFailingUoW) Events() api.EventStore {
	return stepEventFailingStore{EventStore: u.UnitOfWork.Events(), err: u.err}
}

type stepEventFailingStore struct {
	api.EventStore
	err error
}

func (s stepEventFailingStore) AppendEvent(ctx context.Context, event api.Event) error {
	if event.Type == agent.EventStepCompleted {
		return s.err
	}
	return s.EventStore.AppendEvent(ctx, event)
}

func TestAgentWorkerPersistsUsageRecord(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	runner.RegisterAgent(api.AgentProfile{ID: "agent-a"})
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{RunID: "run-usage", RootTaskID: "root", Request: "meter me"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "task-usage",
		Goal:         "summarize",
		OwnerAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a"})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	engine := agent.Engine{Provider: scripted.New([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "done"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete, Usage: provider.Usage{InputTokens: 11, OutputTokens: 7}},
	})}
	if _, err := (AgentWorker{Runner: runner, Engine: engine, AgentID: "agent-a", Model: "scripted"}).ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{Envelope: env}); err != nil {
		t.Fatalf("ExecuteEnvelope() error = %v", err)
	}

	records, err := runner.QueryUsage(ctx, api.UsageSelector{RunID: run.ID})
	if err != nil {
		t.Fatalf("QueryUsage() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("usage records = %d, want exactly 1 (worker must meter the run)", len(records))
	}
	record := records[0]
	if record.TaskID != task.ID || record.AgentID != "agent-a" || record.Model != "scripted" {
		t.Fatalf("usage record identity = %+v", record)
	}
	if record.InputTokens != 11 || record.OutputTokens != 7 {
		t.Fatalf("usage tokens = %d/%d, want 11/7", record.InputTokens, record.OutputTokens)
	}
}

func TestAgentWorkerRestoresRemainingBudgetWithoutDoubleCountingLedger(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{
		RunID:      "run-budget-restore",
		RootTaskID: "root",
		Request:    "resume",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task := api.Task{
		ID:    "task-budget-restore",
		RunID: run.ID,
		Budget: &api.TaskBudget{
			MaxTokens:    10,
			MaxToolCalls: 4,
			MaxSteps:     3,
			MaxWallClock: 10 * time.Second,
		},
	}
	event, err := agent.NewStepCompletedEvent(agent.StepRecord{
		RunID:       run.ID,
		TaskID:      task.ID,
		AgentID:     "agent-a",
		ExecutionID: "lease-old",
		Step: agent.Step{
			Index:      0,
			Decision:   agent.StepDecisionContinue,
			BudgetUsed: agent.BudgetUsage{Tokens: 6, ToolCalls: 2, WallClock: 3 * time.Second},
		},
	})
	if err != nil {
		t.Fatalf("NewStepCompletedEvent() error = %v", err)
	}
	if err := runner.AppendEvent(ctx, event); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	if err := runner.AppendUsage(ctx, api.UsageRecord{
		RunID:       run.ID,
		TaskID:      task.ID,
		TotalTokens: 6,
		ToolCalls:   2,
		Steps:       1,
		DurationMS:  3000,
		Metadata:    map[string]string{"executionId": "lease-old"},
	}); err != nil {
		t.Fatalf("AppendUsage() error = %v", err)
	}

	restored, err := (AgentWorker{Runner: runner}).taskWithRemainingBudget(ctx, task)
	if err != nil {
		t.Fatalf("taskWithRemainingBudget() error = %v", err)
	}
	if got := restored.Budget; got == nil ||
		got.MaxTokens != 4 ||
		got.MaxToolCalls != 2 ||
		got.MaxSteps != 2 ||
		got.MaxWallClock != 7*time.Second {
		t.Fatalf("remaining budget = %#v", got)
	}
}

func TestAgentWorkerRejectsExhaustedBudgetBeforeProviderAfterCrash(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	runner.RegisterAgent(api.AgentProfile{ID: "agent-a"})
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{
		RunID:      "run-budget-exhausted",
		RootTaskID: "root",
		Request:    "resume",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "task-budget-exhausted",
		Goal:         "do not overspend",
		OwnerAgentID: "agent-a",
		Budget:       &api.TaskBudget{MaxTokens: 10},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	event, err := agent.NewStepCompletedEvent(agent.StepRecord{
		RunID:       run.ID,
		TaskID:      task.ID,
		AgentID:     "agent-a",
		ExecutionID: "lease-crashed",
		Step: agent.Step{
			Index:      0,
			Decision:   agent.StepDecisionContinue,
			BudgetUsed: agent.BudgetUsage{Tokens: 10},
		},
	})
	if err != nil {
		t.Fatalf("NewStepCompletedEvent() error = %v", err)
	}
	if err := runner.AppendEvent(ctx, event); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	envelope, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
		RunID:         run.ID,
		TaskID:        task.ID,
		TargetAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	driver := &recordingProvider{}
	outcome, err := (AgentWorker{
		Runner:  runner,
		Engine:  agent.Engine{Provider: driver},
		AgentID: "agent-a",
		Model:   "recording",
	}).ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{Envelope: envelope})
	var failure *agent.AgentFailure
	if !errors.As(err, &failure) || failure.Kind != agent.FailureKindBudgetExhausted {
		t.Fatalf("ExecuteEnvelope() error = %v, want budget_exhausted AgentFailure", err)
	}
	if outcome.State != ExecutionFailed || outcome.Failure != failure {
		t.Fatalf("execution outcome = %#v", outcome)
	}
	if len(driver.requests) != 0 {
		t.Fatalf("provider requests = %d, want 0 after durable budget exhaustion", len(driver.requests))
	}
}
