package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/tool"
)

type boundaryTurnControl struct {
	boundary TurnBoundary
	batch    []ControlMessage
	drained  atomic.Bool
}

func (control *boundaryTurnControl) Drain(_ context.Context, boundary TurnBoundary) ([]ControlMessage, error) {
	if boundary != control.boundary || !control.drained.CompareAndSwap(false, true) {
		return nil, nil
	}
	batch := append([]ControlMessage(nil), control.batch...)
	for index := range batch {
		if batch[index].ID == "" {
			batch[index].ID = "boundary-control"
		}
	}
	return batch, nil
}

func (*boundaryTurnControl) Acknowledge(context.Context, []string) error { return nil }
func (*boundaryTurnControl) Release(context.Context, []string) error     { return nil }
func (*boundaryTurnControl) Interrupts() <-chan struct{}                 { return nil }

type flakyAckTurnControl struct {
	queue *ControlQueue
	fail  atomic.Bool
}

func (control *flakyAckTurnControl) Drain(ctx context.Context, boundary TurnBoundary) ([]ControlMessage, error) {
	return control.queue.Drain(ctx, boundary)
}

func (control *flakyAckTurnControl) Acknowledge(ctx context.Context, ids []string) error {
	if control.fail.CompareAndSwap(true, false) {
		return errors.New("ack store unavailable")
	}
	return control.queue.Acknowledge(ctx, ids)
}

func (control *flakyAckTurnControl) Release(ctx context.Context, ids []string) error {
	return control.queue.Release(ctx, ids)
}

func (control *flakyAckTurnControl) Interrupts() <-chan struct{} {
	return control.queue.Interrupts()
}

type controlledToolDriver struct {
	calls   atomic.Int32
	started chan struct{}
	unknown bool
}

func (driver *controlledToolDriver) Definition() tool.Definition {
	return tool.Definition{Name: "lookup", InputSchema: tool.Schema{Type: "object"}}
}

func (driver *controlledToolDriver) Execute(ctx context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	driver.calls.Add(1)
	if driver.started == nil {
		return tool.Result{ToolCallID: call.ID, Name: call.Name, Content: "unexpected"}, nil
	}
	close(driver.started)
	<-ctx.Done()
	if driver.unknown {
		return tool.Result{}, ctx.Err()
	}
	return tool.Result{}, errors.Join(tool.ErrNotExecuted, ctx.Err())
}

func controlledProviderTurns() [][]provider.Event {
	return [][]provider.Event{
		{
			{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{ID: "call-1", Name: "lookup", Arguments: json.RawMessage(`{}`)}},
			{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
		},
		{
			{Kind: provider.EventTextDelta, TextPhase: provider.TextPhaseFinalAnswer, Text: "corrected answer"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		},
	}
}

func TestTurnControlSteerBeforeToolsSkipsSideEffects(t *testing.T) {
	driver := &controlledToolDriver{}
	control := &boundaryTurnControl{
		boundary: TurnBoundaryBeforeTools,
		batch:    []ControlMessage{{Kind: ControlSteer, Message: message.NewText(message.RoleUser, "change direction")}},
	}
	engine := Engine{Provider: &scriptedProvider{turns: controlledProviderTurns()}, Tools: tool.NewBus(driver)}
	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model: "model", Messages: []message.Message{message.NewText(message.RoleUser, "start")},
		MaxIterations: 2, ToolMode: tool.ModeParallel, Control: control,
	})
	if err != nil {
		t.Fatal(err)
	}
	if driver.calls.Load() != 0 {
		t.Fatalf("tool executed %d times after pre-tool steer", driver.calls.Load())
	}
	assertControlledHistory(t, output.Messages)
}

func TestTurnControlInterruptCancelsRunningToolAndContinues(t *testing.T) {
	queue := NewControlQueue()
	driver := &controlledToolDriver{started: make(chan struct{})}
	engine := Engine{Provider: &scriptedProvider{turns: controlledProviderTurns()}, Tools: tool.NewBus(driver)}
	type outcome struct {
		output LoopOutput
		err    error
	}
	finished := make(chan outcome, 1)
	go func() {
		output, err := engine.RunMessages(context.Background(), LoopInput{
			Model: "model", Messages: []message.Message{message.NewText(message.RoleUser, "start")},
			MaxIterations: 2, ToolMode: tool.ModeParallel, Control: queue,
		})
		finished <- outcome{output: output, err: err}
	}()
	<-driver.started
	if err := queue.Enqueue(ControlMessage{Kind: ControlSteer, Message: message.NewText(message.RoleUser, "stop that tool")}); err != nil {
		t.Fatal(err)
	}
	result := <-finished
	if result.err != nil {
		t.Fatal(result.err)
	}
	if driver.calls.Load() != 1 {
		t.Fatalf("running tool calls = %d, want 1", driver.calls.Load())
	}
	assertControlledHistory(t, result.output.Messages)
}

func TestTurnControlDoesNotHideRunningToolUnknownOutcome(t *testing.T) {
	queue := NewControlQueue()
	driver := &controlledToolDriver{started: make(chan struct{}), unknown: true}
	engine := Engine{Provider: &scriptedProvider{turns: controlledProviderTurns()}, Tools: tool.NewBus(driver)}
	finished := make(chan error, 1)
	go func() {
		_, err := engine.RunMessages(context.Background(), LoopInput{
			Model: "model", Messages: []message.Message{message.NewText(message.RoleUser, "start")},
			MaxIterations: 2, ToolMode: tool.ModeParallel, Control: queue,
		})
		finished <- err
	}()
	<-driver.started
	if err := queue.Enqueue(ControlMessage{Kind: ControlSteer, Message: message.NewText(message.RoleUser, "stop")}); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("unknown running tool outcome error = %v", err)
	}
}

func TestTurnControlFollowUpRunsAfterCompletedAnswer(t *testing.T) {
	queue := NewControlQueue()
	if err := queue.Enqueue(ControlMessage{Kind: ControlFollowUp, Message: message.NewText(message.RoleUser, "one more question")}); err != nil {
		t.Fatal(err)
	}
	providerDriver := &scriptedProvider{turns: [][]provider.Event{
		{{Kind: provider.EventTextDelta, Text: "first answer"}, {Kind: provider.EventDone, StopReason: provider.StopReasonComplete}},
		{{Kind: provider.EventTextDelta, Text: "second answer"}, {Kind: provider.EventDone, StopReason: provider.StopReasonComplete}},
	}}
	var checkpoints []TurnCheckpoint
	output, err := (Engine{Provider: providerDriver}).RunMessages(context.Background(), LoopInput{
		Model: "model", Messages: []message.Message{message.NewText(message.RoleUser, "start")}, MaxIterations: 2, Control: queue,
		CheckpointRecorder: CheckpointRecorderFunc(func(_ context.Context, checkpoint TurnCheckpoint) error {
			checkpoints = append(checkpoints, checkpoint)
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(providerDriver.requests) != 2 || output.Messages[len(output.Messages)-1].Text != "second answer" {
		t.Fatalf("follow-up output=%#v requests=%d", output.Messages, len(providerDriver.requests))
	}
	secondRequest := providerDriver.requests[1].Messages
	if len(secondRequest) < 2 || secondRequest[len(secondRequest)-1].Text != "one more question" || secondRequest[len(secondRequest)-2].Text != "first answer" {
		t.Fatalf("follow-up request history = %#v", secondRequest)
	}
	if len(checkpoints) < 1 {
		t.Fatal("follow-up boundary was not durably checkpointed")
	}
	firstCheckpoint := checkpoints[0].Messages
	if len(firstCheckpoint) < 2 || firstCheckpoint[len(firstCheckpoint)-1].Text != "one more question" ||
		firstCheckpoint[len(firstCheckpoint)-2].Text != "first answer" {
		t.Fatalf("checkpointed follow-up history = %#v", firstCheckpoint)
	}
}

func TestTurnControlAbortStopsAtAnswerBoundary(t *testing.T) {
	control := &boundaryTurnControl{boundary: TurnBoundaryAfterAnswer, batch: []ControlMessage{{Kind: ControlAbort}}}
	providerDriver := &scriptedProvider{turns: [][]provider.Event{{
		{Kind: provider.EventTextDelta, Text: "answer"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}}}
	var checkpoint TurnCheckpoint
	_, err := (Engine{Provider: providerDriver}).RunMessages(context.Background(), LoopInput{
		Model: "model", Messages: []message.Message{message.NewText(message.RoleUser, "start")}, MaxIterations: 1, Control: control,
		CheckpointRecorder: CheckpointRecorderFunc(func(_ context.Context, current TurnCheckpoint) error {
			checkpoint = current
			return nil
		}),
	})
	if !errors.Is(err, ErrTurnControlAbort) {
		t.Fatalf("abort error = %v", err)
	}
	if !checkpoint.ControlAborted || len(checkpoint.AppliedControlIDs) != 1 {
		t.Fatalf("abort checkpoint = %#v", checkpoint)
	}
}

func assertControlledHistory(t *testing.T, history []message.Message) {
	t.Helper()
	foundCancelled, foundSteer := false, false
	for _, current := range history {
		if current.ToolResult != nil && current.ToolResult.IsError && strings.Contains(current.ToolResult.Content, "cancelled") {
			foundCancelled = true
		}
		if current.Role == message.RoleUser && (current.Text == "change direction" || current.Text == "stop that tool") {
			foundSteer = true
		}
	}
	if !foundCancelled || !foundSteer || history[len(history)-1].Text != "corrected answer" {
		t.Fatalf("controlled history = %#v", history)
	}
}

func TestTurnControlProviderFailureReleasesReservedSteerForRetry(t *testing.T) {
	queue := NewControlQueue()
	if err := queue.Enqueue(ControlMessage{
		ID: "steer-1", Kind: ControlSteer,
		Message: message.NewText(message.RoleUser, "retry this steer"),
	}); err != nil {
		t.Fatal(err)
	}
	providerDriver := &scriptedProvider{turns: [][]provider.Event{
		{{Kind: provider.EventError, Err: errors.New("provider disconnected")}},
		{{Kind: provider.EventTextDelta, Text: "recovered"}, {Kind: provider.EventDone, StopReason: provider.StopReasonComplete}},
	}}
	engine := Engine{Provider: providerDriver}
	input := LoopInput{
		Model: "model", Messages: []message.Message{message.NewText(message.RoleUser, "start")},
		MaxIterations: 1, Control: queue,
	}
	if _, err := engine.RunMessages(context.Background(), input); err == nil {
		t.Fatal("provider failure unexpectedly succeeded")
	}
	output, err := engine.RunMessages(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if output.Messages[len(output.Messages)-1].Text != "recovered" {
		t.Fatalf("retry output = %#v", output.Messages)
	}
	retryRequest := providerDriver.requests[1].Messages
	found := false
	for _, current := range retryRequest {
		found = found || current.Text == "retry this steer"
	}
	if !found {
		t.Fatalf("released steer missing from retry request: %#v", retryRequest)
	}
	if pending, err := queue.Drain(context.Background(), TurnBoundaryAfterAnswer); err != nil || len(pending) != 0 {
		t.Fatalf("acknowledged steer remained pending: %#v, %v", pending, err)
	}
}

func TestTurnControlCheckpointReconcilesFailedAcknowledgementWithoutDuplicate(t *testing.T) {
	queue := NewControlQueue()
	if err := queue.Enqueue(ControlMessage{
		ID: "follow-1", Kind: ControlFollowUp,
		Message: message.NewText(message.RoleUser, "one durable follow-up"),
	}); err != nil {
		t.Fatal(err)
	}
	control := &flakyAckTurnControl{queue: queue}
	control.fail.Store(true)
	providerDriver := &scriptedProvider{turns: [][]provider.Event{
		{{Kind: provider.EventTextDelta, Text: "first answer"}, {Kind: provider.EventDone, StopReason: provider.StopReasonComplete}},
		{{Kind: provider.EventTextDelta, Text: "retry answer"}, {Kind: provider.EventDone, StopReason: provider.StopReasonComplete}},
	}}
	var checkpoint TurnCheckpoint
	input := LoopInput{
		Model: "model", Messages: []message.Message{message.NewText(message.RoleUser, "start")},
		MaxIterations: 2, Control: control,
		CheckpointRecorder: CheckpointRecorderFunc(func(_ context.Context, current TurnCheckpoint) error {
			checkpoint = current
			return nil
		}),
	}
	first, err := (Engine{Provider: providerDriver}).RunMessages(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "ack store unavailable") {
		t.Fatalf("ack failure = %v", err)
	}
	if len(checkpoint.AppliedControlIDs) != 1 || checkpoint.AppliedControlIDs[0] != "follow-1" {
		t.Fatalf("checkpoint omitted applied control ids: %#v", checkpoint)
	}
	input.Messages = first.Messages
	input.AppliedControlIDs = append([]string(nil), checkpoint.AppliedControlIDs...)
	input.MaxIterations = 1
	second, err := (Engine{Provider: providerDriver}).RunMessages(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if second.Messages[len(second.Messages)-1].Text != "retry answer" {
		t.Fatalf("retry output = %#v", second.Messages)
	}
	count := 0
	for _, current := range providerDriver.requests[1].Messages {
		if current.Text == "one durable follow-up" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("checkpointed control appeared %d times in retry request: %#v", count, providerDriver.requests[1].Messages)
	}
}
