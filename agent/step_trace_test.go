package agent

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/tool"
	"github.com/Viking602/venat/tool/kit"
)

// requireNoStepTimestamps asserts that the loop left every Step's wall-clock
// fields zero. A replayed loop must reproduce Steps byte-for-byte (ADR-007),
// so the loop is forbidden from stamping time.Now() onto them.
func requireNoStepTimestamps(t *testing.T, steps []Step) {
	t.Helper()
	for _, step := range steps {
		if !step.StartedAt.IsZero() || !step.EndedAt.IsZero() {
			t.Fatalf("step %d carries wall-clock timestamps (StartedAt=%v EndedAt=%v); Steps must stay replay-deterministic",
				step.Index, step.StartedAt, step.EndedAt)
		}
	}
}

func TestRunMessagesEmitsStepPerIterationToolThenFinish(t *testing.T) {
	driver, err := kit.Tool("lookup", func(_ context.Context, input struct {
		Query string `json:"query"`
	},
	) (string, error) {
		return "result:" + input.Query, nil
	})
	if err != nil {
		t.Fatalf("tool setup: %v", err)
	}
	engine := Engine{Provider: fakeProvider{}, Tools: tool.NewBus(driver)}

	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "find venat")},
		MaxIterations: 3,
		ToolMode:      tool.ModeSequential,
	})
	if err != nil {
		t.Fatalf("RunMessages() error = %v", err)
	}

	// fakeProvider emits one tool call (turn 1) then prose (turn 2): two
	// iterations, so two steps.
	if output.Iterations != 2 {
		t.Fatalf("Iterations = %d, want 2", output.Iterations)
	}
	if len(output.Steps) != output.Iterations {
		t.Fatalf("len(Steps) = %d, want one per iteration (%d)", len(output.Steps), output.Iterations)
	}
	if output.ToolCallsUsed != 1 {
		t.Fatalf("ToolCallsUsed = %d, want 1", output.ToolCallsUsed)
	}

	tool0 := output.Steps[0]
	if tool0.Index != 0 || tool0.Decision != StepDecisionContinue {
		t.Fatalf("step 0 = {Index:%d Decision:%s}, want {0 continue}", tool0.Index, tool0.Decision)
	}
	if tool0.ModelCall == nil || tool0.ModelCall.Model != "test-model" {
		t.Fatalf("step 0 ModelCall = %#v, want model test-model", tool0.ModelCall)
	}
	if len(tool0.ToolCalls) != 1 || tool0.ToolCalls[0].Name != "lookup" {
		t.Fatalf("step 0 ToolCalls = %#v, want one lookup trace", tool0.ToolCalls)
	}
	if len(tool0.ToolCalls[0].Output) == 0 {
		t.Fatalf("step 0 tool trace output is empty, want the tool result")
	}
	if tool0.BudgetUsed.ToolCalls != 1 {
		t.Fatalf("step 0 BudgetUsed.ToolCalls = %d, want 1", tool0.BudgetUsed.ToolCalls)
	}

	finish := output.Steps[1]
	if finish.Index != 1 || finish.Decision != StepDecisionFinish {
		t.Fatalf("step 1 = {Index:%d Decision:%s}, want {1 finish}", finish.Index, finish.Decision)
	}
	if len(finish.ToolCalls) != 0 {
		t.Fatalf("step 1 ToolCalls = %#v, want none on the finishing turn", finish.ToolCalls)
	}

	requireNoStepTimestamps(t, output.Steps)
}

func TestRunMessagesEmitsStepPerIterationAtMaxTurns(t *testing.T) {
	driver, err := kit.Tool("lookup", func(_ context.Context, _ struct {
		Query string `json:"query"`
	},
	) (string, error) {
		return "result", nil
	})
	if err != nil {
		t.Fatalf("tool setup: %v", err)
	}
	engine := Engine{Provider: &alwaysToolProvider{}, Tools: tool.NewBus(driver)}

	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model:    "test-model",
		Messages: []message.Message{message.NewText(message.RoleUser, "loop forever")},
		// MaxIterations unset -> default ceiling of 12.
	})
	if err != nil {
		t.Fatalf("RunMessages() error = %v", err)
	}

	if len(output.Steps) != output.Iterations || output.Iterations != 12 {
		t.Fatalf("len(Steps)=%d Iterations=%d, want both 12", len(output.Steps), output.Iterations)
	}
	if output.ToolCallsUsed != 12 {
		t.Fatalf("ToolCallsUsed = %d, want 12", output.ToolCallsUsed)
	}
	for i, step := range output.Steps {
		if step.Index != i {
			t.Fatalf("step %d has non-contiguous Index %d", i, step.Index)
		}
		if step.Decision != StepDecisionContinue {
			t.Fatalf("step %d Decision = %s, want continue (never reached a terminal turn)", i, step.Decision)
		}
		if step.BudgetUsed.ToolCalls != i+1 {
			t.Fatalf("step %d BudgetUsed.ToolCalls = %d, want cumulative %d", i, step.BudgetUsed.ToolCalls, i+1)
		}
	}
	requireNoStepTimestamps(t, output.Steps)
}

func TestRunMessagesStepDecisionFinishOnTerminalTool(t *testing.T) {
	driver := &scriptedProvider{
		turns: [][]provider.Event{{
			{
				Kind: provider.EventToolCall,
				ToolCall: &message.ToolCall{
					ID:        "call-1",
					Name:      "submit_report",
					Arguments: json.RawMessage(`{"answer":"done"}`),
				},
			},
			{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
		}},
	}
	engine := Engine{Provider: driver, Tools: tool.NewBus(terminalTool{})}

	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "finish")},
		MaxIterations: 3,
	})
	if err != nil {
		t.Fatalf("RunMessages() error = %v", err)
	}

	if len(output.Steps) != 1 {
		t.Fatalf("len(Steps) = %d, want 1 (terminal tool stops after first turn)", len(output.Steps))
	}
	step := output.Steps[0]
	if step.Decision != StepDecisionFinish {
		t.Fatalf("terminal-tool step Decision = %s, want finish", step.Decision)
	}
	if len(step.ToolCalls) != 1 || step.ToolCalls[0].Name != "submit_report" {
		t.Fatalf("terminal-tool step ToolCalls = %#v, want one submit_report trace", step.ToolCalls)
	}
	if string(step.ToolCalls[0].Output) != `{"answer":"done"}` {
		t.Fatalf("terminal-tool trace Output = %s, want the submitted arguments", step.ToolCalls[0].Output)
	}
	if output.ToolCallsUsed != 1 {
		t.Fatalf("ToolCallsUsed = %d, want 1", output.ToolCallsUsed)
	}
	requireNoStepTimestamps(t, output.Steps)
}

func TestEngineRepairConcatenatesStepsWithGlobalReindex(t *testing.T) {
	// Initial run returns enum-invalid output; the single repair run fixes it.
	// Each run is one finishing turn, so the merged trace must be two steps
	// with globally continuous indices 0 and 1 — not two index-0 collisions.
	_, engine := newOutputPolicyEngine(
		`{"status":"blocked","score":0.75,"count":2,"tags":["risk"],"accepted":true}`,
		`{"status":"ok","score":0.75,"count":2,"tags":["risk"],"accepted":true}`,
	)
	policy := outputPolicyReport()
	policy.Repair = true
	policy.MaxRepairAttempts = 1

	result := engine.Run(context.Background(), api.Task{Goal: "classify risk"}, policy)

	if !result.Valid {
		t.Fatalf("result.Valid = false, failure = %#v", result.Failure)
	}
	if result.RepairCount != 1 {
		t.Fatalf("RepairCount = %d, want 1", result.RepairCount)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("len(Steps) = %d, want 2 (initial + repair turn)", len(result.Steps))
	}
	for i, step := range result.Steps {
		if step.Index != i {
			t.Fatalf("step %d has Index %d; repair steps must be globally reindexed", i, step.Index)
		}
		if step.Decision != StepDecisionFinish {
			t.Fatalf("step %d Decision = %s, want finish", i, step.Decision)
		}
	}
	requireNoStepTimestamps(t, result.Steps)
}

func TestRunMessagesRecordsFinalizedSteps(t *testing.T) {
	engine := newLoopToolEngine(t, &alwaysToolProvider{})
	var recorded []Step
	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model:    "test-model",
		Messages: []message.Message{message.NewText(message.RoleUser, "loop")},
		StepPolicy: stepPolicyFunc(func(LoopSnapshot) (StepDecision, error) {
			return StepDecisionHandoff, nil
		}),
		StepRecorder: StepRecorderFunc(func(_ context.Context, step Step) error {
			recorded = append(recorded, step)
			return nil
		}),
	})
	if err != nil {
		t.Fatalf("RunMessages() error = %v", err)
	}
	if len(recorded) != 1 {
		t.Fatalf("recorded %d steps, want 1", len(recorded))
	}
	if recorded[0].Decision != StepDecisionHandoff {
		t.Fatalf("recorded decision = %q, want handoff after StepPolicy mutation", recorded[0].Decision)
	}
	if !reflect.DeepEqual(recorded, output.Steps) {
		t.Fatalf("recorded steps = %#v, output steps = %#v", recorded, output.Steps)
	}
}

func TestRunMessagesRecorderFailureReturnsPartialTrace(t *testing.T) {
	t.Run("recorder failure", func(t *testing.T) {
		recordErr := errors.New("store unavailable")
		engine := Engine{Provider: singleTurnProvider("done")}
		output, err := engine.RunMessages(context.Background(), LoopInput{
			Model:    "test-model",
			Messages: []message.Message{message.NewText(message.RoleUser, "finish")},
			StepRecorder: StepRecorderFunc(func(context.Context, Step) error {
				return recordErr
			}),
		})
		if !errors.Is(err, recordErr) {
			t.Fatalf("RunMessages() error = %v, want recorder cause", err)
		}
		if !strings.Contains(err.Error(), "agent: record step 0:") {
			t.Fatalf("RunMessages() error = %q, want indexed recorder prefix", err)
		}
		if len(output.Steps) != 1 || output.Steps[0].Decision != StepDecisionFinish {
			t.Fatalf("partial Steps = %#v, want finalized finish step", output.Steps)
		}
		if output.StopReason != provider.StopReasonError {
			t.Fatalf("partial StopReason = %q, want error", output.StopReason)
		}
	})

	t.Run("policy and recorder failures join", func(t *testing.T) {
		policyErr := errors.New("policy failed")
		recordErr := errors.New("record failed")
		engine := newLoopToolEngine(t, &alwaysToolProvider{})
		output, err := engine.RunMessages(context.Background(), LoopInput{
			Model:    "test-model",
			Messages: []message.Message{message.NewText(message.RoleUser, "loop")},
			StepPolicy: stepPolicyFunc(func(LoopSnapshot) (StepDecision, error) {
				return "", policyErr
			}),
			StepRecorder: StepRecorderFunc(func(context.Context, Step) error {
				return recordErr
			}),
		})
		if !errors.Is(err, policyErr) || !errors.Is(err, recordErr) || !errors.Is(err, ErrStepAborted) {
			t.Fatalf("RunMessages() error = %v, want policy, recorder, and ErrStepAborted causes", err)
		}
		if len(output.Steps) != 1 {
			t.Fatalf("partial Steps = %#v, want one finalized step", output.Steps)
		}
	})
}

func TestEngineRepairStepRecorderUsesGlobalIndexes(t *testing.T) {
	_, engine := newOutputPolicyEngine(
		`{"status":"blocked","score":0.75,"count":2,"tags":["risk"],"accepted":true}`,
		`{"status":"ok","score":0.75,"count":2,"tags":["risk"],"accepted":true}`,
	)
	var recorded []Step
	engine.StepRecorder = StepRecorderFunc(func(_ context.Context, step Step) error {
		recorded = append(recorded, step)
		return nil
	})
	policy := outputPolicyReport()
	policy.Repair = true
	policy.MaxRepairAttempts = 1

	result := engine.Run(context.Background(), api.Task{Goal: "classify risk"}, policy)
	if result.Failure != nil {
		t.Fatalf("Run() failure = %#v", result.Failure)
	}
	if len(recorded) != 2 {
		t.Fatalf("recorded %d steps, want initial and repair steps", len(recorded))
	}
	for index, step := range recorded {
		if step.Index != index {
			t.Fatalf("recorded step %d has Index %d, want global index %d", index, step.Index, index)
		}
	}
}

func TestStepCompletedEventRoundTrip(t *testing.T) {
	record := StepRecord{
		RunID:       "run-1",
		TaskID:      "task-1",
		AgentID:     "agent-1",
		ExecutionID: "lease-1",
		Step:        Step{Index: 0, Decision: StepDecisionFinish},
	}
	event, err := NewStepCompletedEvent(record)
	if err != nil {
		t.Fatalf("NewStepCompletedEvent() error = %v", err)
	}
	if event.Type != EventStepCompleted || event.RunID != record.RunID || event.TaskID != record.TaskID {
		t.Fatalf("event identity = %#v, want type/run/task mirrored", event)
	}
	if event.RecordedAt.Location() != time.UTC {
		t.Fatalf("RecordedAt location = %v, want UTC", event.RecordedAt.Location())
	}

	direct, err := ReconstructStepTrace([]api.Event{event}, StepSelector{})
	if err != nil {
		t.Fatalf("ReconstructStepTrace(direct) error = %v", err)
	}
	if !reflect.DeepEqual(direct, []StepRecord{record}) {
		t.Fatalf("direct records = %#v, want %#v", direct, []StepRecord{record})
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("json.Marshal(event) error = %v", err)
	}
	var roundTripped api.Event
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("json.Unmarshal(event) error = %v", err)
	}
	decoded, err := ReconstructStepTrace([]api.Event{roundTripped}, StepSelector{})
	if err != nil {
		t.Fatalf("ReconstructStepTrace(JSON map) error = %v", err)
	}
	if !reflect.DeepEqual(decoded, []StepRecord{record}) {
		t.Fatalf("round-tripped records = %#v, want %#v", decoded, []StepRecord{record})
	}

	invalid := []StepRecord{
		{TaskID: "task", AgentID: "agent", ExecutionID: "exec", Step: Step{Index: 0}},
		{RunID: "run", AgentID: "agent", ExecutionID: "exec", Step: Step{Index: 0}},
		{RunID: "run", TaskID: "task", ExecutionID: "exec", Step: Step{Index: 0}},
		{RunID: "run", TaskID: "task", AgentID: "agent", Step: Step{Index: 0}},
		{RunID: "run", TaskID: "task", AgentID: "agent", ExecutionID: "exec", Step: Step{Index: -1}},
	}
	for index, bad := range invalid {
		if _, err := NewStepCompletedEvent(bad); !errors.Is(err, ErrInvalidStepEvent) {
			t.Fatalf("invalid record %d error = %v, want ErrInvalidStepEvent", index, err)
		}
	}
}

func TestReconstructStepTrace(t *testing.T) {
	newEvent := func(record StepRecord, sequence int) api.Event {
		event, err := NewStepCompletedEvent(record)
		if err != nil {
			t.Fatalf("NewStepCompletedEvent(%#v) error = %v", record, err)
		}
		event.Sequence = sequence
		return event
	}
	record := func(runID, taskID, agentID, executionID string, index int) StepRecord {
		return StepRecord{
			RunID:       runID,
			TaskID:      taskID,
			AgentID:     agentID,
			ExecutionID: executionID,
			Step:        Step{Index: index, Decision: StepDecisionContinue},
		}
	}

	a0 := record("run-1", "task-1", "agent-1", "exec-a", 0)
	b0 := record("run-1", "task-1", "agent-1", "exec-b", 0)
	a1 := record("run-1", "task-1", "agent-1", "exec-a", 1)
	otherRun := record("run-2", "task-1", "agent-1", "exec-c", 0)
	otherTask := record("run-1", "task-2", "agent-1", "exec-d", 0)
	otherAgent := record("run-1", "task-1", "agent-2", "exec-e", 0)
	events := []api.Event{
		newEvent(a0, 1),
		{Type: api.EventTaskCreated, RunID: "run-1", Sequence: 2},
		newEvent(b0, 3),
		newEvent(a1, 4),
		newEvent(otherRun, 5),
		newEvent(otherTask, 6),
		newEvent(otherAgent, 7),
	}

	tests := []struct {
		name     string
		events   []api.Event
		selector StepSelector
		want     []StepRecord
		wantErr  bool
	}{
		{
			name:     "separate executions preserve event order",
			events:   events[:4],
			selector: StepSelector{},
			want:     []StepRecord{a0, b0, a1},
		},
		{
			name:     "all selector fields filter",
			events:   events,
			selector: StepSelector{RunID: "run-1", TaskID: "task-1", AgentID: "agent-1", ExecutionID: "exec-a"},
			want:     []StepRecord{a0, a1},
		},
		{
			name:     "run selector",
			events:   events,
			selector: StepSelector{RunID: "run-2"},
			want:     []StepRecord{otherRun},
		},
		{
			name:     "task selector",
			events:   events,
			selector: StepSelector{TaskID: "task-2"},
			want:     []StepRecord{otherTask},
		},
		{
			name:     "agent selector",
			events:   events,
			selector: StepSelector{AgentID: "agent-2"},
			want:     []StepRecord{otherAgent},
		},
		{
			name: "malformed payload",
			events: []api.Event{{
				RunID: "run-1", TaskID: "task-1", Type: EventStepCompleted,
				Payload: map[string]any{"record": "bad"},
			}},
			wantErr: true,
		},
		{
			name: "mismatched event IDs",
			events: []api.Event{func() api.Event {
				event := newEvent(a0, 1)
				event.TaskID = "different"
				return event
			}()},
			wantErr: true,
		},
		{
			name:    "duplicate index",
			events:  []api.Event{newEvent(a0, 1), newEvent(a0, 2)},
			wantErr: true,
		},
		{
			name:    "non-zero first index",
			events:  []api.Event{newEvent(a1, 1)},
			wantErr: true,
		},
		{
			name: "gapped index",
			events: []api.Event{
				newEvent(a0, 1),
				newEvent(record("run-1", "task-1", "agent-1", "exec-a", 2), 2),
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ReconstructStepTrace(test.events, test.selector)
			if test.wantErr {
				if !errors.Is(err, ErrInvalidStepEvent) {
					t.Fatalf("ReconstructStepTrace() error = %v, want ErrInvalidStepEvent", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReconstructStepTrace() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ReconstructStepTrace() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestReconstructExecutionCheckpointsFiltersAndPreservesOrder(t *testing.T) {
	first := ExecutionCheckpointRecord{
		RunID: "run-1", TaskID: "task-1", AgentID: "agent-1", ExecutionID: "lease-1",
		Checkpoint: TurnCheckpoint{
			Messages: []message.Message{message.NewText(message.RoleAssistant, "first")},
			Step:     Step{Index: 0, Decision: StepDecisionContinue},
		},
	}
	second := first
	second.ExecutionID = "lease-2"
	second.Checkpoint = TurnCheckpoint{
		Messages: []message.Message{message.NewText(message.RoleAssistant, "second")},
		Step:     Step{Index: 0, Decision: StepDecisionFinish},
	}
	otherAgent := first
	otherAgent.AgentID = "agent-2"

	events := make([]api.Event, 0, 3)
	for _, record := range []ExecutionCheckpointRecord{first, otherAgent, second} {
		event, err := NewExecutionCheckpointedEvent(record)
		if err != nil {
			t.Fatalf("NewExecutionCheckpointedEvent() error = %v", err)
		}
		events = append(events, event)
	}

	got, err := ReconstructExecutionCheckpoints(events, StepSelector{
		RunID: "run-1", TaskID: "task-1", AgentID: "agent-1",
	})
	if err != nil {
		t.Fatalf("ReconstructExecutionCheckpoints() error = %v", err)
	}
	want := []ExecutionCheckpointRecord{first, second}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReconstructExecutionCheckpoints() = %#v, want %#v", got, want)
	}
}

func TestLatestExecutionCheckpointFailsClosedOnCorruptReplayState(t *testing.T) {
	base := ExecutionCheckpointRecord{
		RunID: "run-1", TaskID: "task-1", AgentID: "agent-1", ExecutionID: "lease-1",
		Checkpoint: TurnCheckpoint{
			Messages: []message.Message{message.NewText(message.RoleAssistant, "done")},
			Step:     Step{Index: 0, Decision: StepDecisionFinish},
		},
	}
	eventFor := func(record ExecutionCheckpointRecord) api.Event {
		return api.Event{
			RunID: record.RunID, TaskID: record.TaskID, Type: EventExecutionCheckpointed,
			Payload: map[string]any{"record": record},
		}
	}
	pending := base
	pending.Checkpoint = TurnCheckpoint{
		Messages: []message.Message{
			message.NewText(message.RoleUser, "run tools"),
			{Role: message.RoleAssistant, ToolCalls: []message.ToolCall{
				{ID: "call-1", Name: "write"},
				{ID: "call-2", Name: "write"},
			}},
			message.NewToolResult(message.ToolResult{ToolCallID: "call-1", Name: "write", Content: "ok"}),
		},
		Step: Step{Index: 0}, ToolCallsUsed: 2, PendingToolCalls: true,
	}
	if got, found, err := LatestExecutionCheckpoint(
		[]api.Event{eventFor(pending)}, StepSelector{RunID: base.RunID, TaskID: base.TaskID},
	); err != nil || !found || !reflect.DeepEqual(got, pending) {
		t.Fatalf("valid pending checkpoint got=%#v found=%v error=%v", got, found, err)
	}

	negativeUsage := base
	negativeUsage.Checkpoint.Usage.InputTokens = -1
	incompleteCompleted := base
	incompleteCompleted.Checkpoint.Messages = pending.Checkpoint.Messages[:2]
	fullyResolvedPending := pending
	fullyResolvedPending.Checkpoint.Messages = append(
		append([]message.Message(nil), pending.Checkpoint.Messages...),
		message.NewToolResult(message.ToolResult{ToolCallID: "call-2", Name: "write", Content: "ok"}),
	)
	duplicatePendingResult := pending
	duplicatePendingResult.Checkpoint.Messages = append(
		append([]message.Message(nil), pending.Checkpoint.Messages...),
		message.NewToolResult(message.ToolResult{ToolCallID: "call-1", Name: "write", Content: "duplicate"}),
	)

	tests := []struct {
		name  string
		event api.Event
	}{
		{name: "malformed payload", event: api.Event{RunID: base.RunID, TaskID: base.TaskID, Type: EventExecutionCheckpointed, Payload: map[string]any{"record": "bad"}}},
		{name: "identity mismatch", event: func() api.Event {
			event := eventFor(base)
			event.TaskID = "other-task"
			return event
		}()},
		{name: "negative usage", event: eventFor(negativeUsage)},
		{name: "incomplete transcript marked complete", event: eventFor(incompleteCompleted)},
		{name: "pending flag with no unresolved calls", event: eventFor(fullyResolvedPending)},
		{name: "duplicate result in pending turn", event: eventFor(duplicatePendingResult)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := LatestExecutionCheckpoint(
				[]api.Event{test.event}, StepSelector{RunID: base.RunID},
			); !errors.Is(err, ErrInvalidCheckpointEvent) {
				t.Fatalf("LatestExecutionCheckpoint() error = %v, want ErrInvalidCheckpointEvent", err)
			}
		})
	}
}

type operationRecordingDriver struct {
	calls []tool.Call
}

func (d *operationRecordingDriver) Definition() tool.Definition {
	return tool.Definition{Name: "write"}
}

func (d *operationRecordingDriver) Execute(_ context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	d.calls = append(d.calls, call)
	return tool.Result{ToolCallID: call.ID, Name: call.Name, Content: "ok"}, nil
}

func TestEngineAssignsStableDistinctToolOperationSlots(t *testing.T) {
	driver := &operationRecordingDriver{}
	engine := Engine{
		Provider: &scriptedProvider{turns: [][]provider.Event{
			{
				{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{ID: "provider-a", Name: "write", Arguments: json.RawMessage(`{"same":true}`)}},
				{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{ID: "provider-b", Name: "write", Arguments: json.RawMessage(`{"same":true}`)}},
				{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
			},
			{
				{Kind: provider.EventTextDelta, Text: "done"},
				{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
			},
		}},
		Tools:         tool.NewBus(driver),
		LoopPolicy:    LoopPolicy{MaxIterations: 2},
		OperationTurn: 2,
	}
	result := engine.Run(context.Background(), api.Task{Goal: "write twice"}, OutputPolicy{})
	if result.Failure != nil {
		t.Fatalf("Engine.Run() failure = %v", result.Failure)
	}
	if len(driver.calls) != 2 {
		t.Fatalf("driver calls = %d, want 2", len(driver.calls))
	}
	if driver.calls[0].OperationID != "turn:2:call:0" || driver.calls[1].OperationID != "turn:2:call:1" {
		t.Fatalf("operation IDs = %q, %q", driver.calls[0].OperationID, driver.calls[1].OperationID)
	}
	if next := nextToolOperationTurn(result.Messages); next != 3 {
		t.Fatalf("next operation turn = %d, want 3", next)
	}
	if driver.calls[0].OperationID == driver.calls[1].OperationID {
		t.Fatal("identical repeated calls collapsed onto one logical operation slot")
	}
	for _, current := range result.Messages {
		if current.Role == message.RoleAssistant && len(current.ToolCalls) == 2 {
			if current.ToolCalls[0].OperationID != driver.calls[0].OperationID ||
				current.ToolCalls[1].OperationID != driver.calls[1].OperationID {
				t.Fatalf("checkpoint transcript lost operation IDs: %#v", current.ToolCalls)
			}
			return
		}
	}
	t.Fatal("assistant tool-call turn missing from result")
}
