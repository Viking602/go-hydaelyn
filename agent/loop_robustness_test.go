package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/hook"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/stream"
	"github.com/Viking602/go-hydaelyn/tool"
	"github.com/Viking602/go-hydaelyn/tool/kit"
)

// TestRunRecoversGuardrailPanicAsEngineError pins the loop's panic backstop:
// caller-supplied extension code (here an output guardrail) that panics must
// degrade to a typed FailureKindEngineError carrying ErrPanicRecovered, not
// crash the worker. If the backstop were missing this test would abort the
// package binary instead of failing an assertion.
func TestRunRecoversGuardrailPanicAsEngineError(t *testing.T) {
	engine := Engine{
		Provider: singleTurnProvider("answer"),
		Model:    "test-model",
		OutputGuardrails: []OutputGuardrail{
			NewOutputGuardrail("boom", func(_ context.Context, _ OutputGuardrailInput) (OutputGuardrailResult, error) {
				panic("guardrail exploded")
			}),
		},
	}

	result := engine.Run(context.Background(), api.Task{Goal: "do the thing"}, OutputPolicy{})

	if result.Failure == nil {
		t.Fatal("expected a failure when a guardrail panics")
	}
	if result.Failure.Kind != FailureKindEngineError {
		t.Fatalf("Failure.Kind = %s, want engine_error", result.Failure.Kind)
	}
	if !errors.Is(result.Failure, ErrPanicRecovered) {
		t.Fatalf("errors.Is(Failure, ErrPanicRecovered) = false, Failure = %v", result.Failure)
	}
}

// panicBeforeModelHook panics from BeforeModelCall, the first hook stage a
// no-tool turn reaches, so the test can prove a hook panic is contained by
// hook.Chain and surfaces end-to-end as a typed failure.
type panicBeforeModelHook struct{}

func (panicBeforeModelHook) TransformContext(_ context.Context, m []message.Message) ([]message.Message, error) {
	return m, nil
}
func (panicBeforeModelHook) BeforeModelCall(_ context.Context, _ *provider.Request) error {
	panic("hook exploded")
}
func (panicBeforeModelHook) BeforeToolCall(_ context.Context, _ *tool.Call) error  { return nil }
func (panicBeforeModelHook) AfterToolCall(_ context.Context, _ *tool.Result) error { return nil }
func (panicBeforeModelHook) OnEvent(_ context.Context, _ provider.Event) error     { return nil }

// TestRunRecoversHookPanicAsEngineError pins the end-to-end path for a
// panicking hook: hook.Chain converts it to an ErrHandlerPanic error one level
// down, and Engine.Run reports it as an engine_error whose cause still walks to
// hook.ErrHandlerPanic.
func TestRunRecoversHookPanicAsEngineError(t *testing.T) {
	engine := Engine{
		Provider: singleTurnProvider("answer"),
		Model:    "test-model",
		Hooks:    hook.NewChain(panicBeforeModelHook{}),
	}

	result := engine.Run(context.Background(), api.Task{Goal: "do the thing"}, OutputPolicy{})

	if result.Failure == nil {
		t.Fatal("expected a failure when a hook panics")
	}
	if result.Failure.Kind != FailureKindEngineError {
		t.Fatalf("Failure.Kind = %s, want engine_error", result.Failure.Kind)
	}
	if !errors.Is(result.Failure, hook.ErrHandlerPanic) {
		t.Fatalf("errors.Is(Failure, hook.ErrHandlerPanic) = false, Failure = %v", result.Failure)
	}
}

// erroringSecondTurnProvider serves a tool call on the first turn and fails on
// the second, so the loop accumulates a real trace (assistant tool call + tool
// result + one Step) before the error lands.
type erroringSecondTurnProvider struct{ calls int }

func (*erroringSecondTurnProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "err-second-turn"}
}

func (p *erroringSecondTurnProvider) Stream(_ context.Context, _ provider.Request) (provider.Stream, error) {
	p.calls++
	if p.calls == 1 {
		return provider.NewSliceStream([]provider.Event{
			{
				Kind: provider.EventToolCall,
				ToolCall: &message.ToolCall{
					ID:        "call-1",
					Name:      "lookup",
					Arguments: json.RawMessage(`{"query":"x"}`),
				},
			},
			{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
		}), nil
	}
	return nil, errors.New("provider down on turn 2")
}

// TestRunMessagesPreservesTraceOnNonBudgetError pins that a non-budget loop
// error returns the partial trace accumulated so far rather than an empty
// LoopOutput: the messages and steps from completed turns survive so a
// scheduler and the durable record keep the context that led up to the failure.
func TestRunMessagesPreservesTraceOnNonBudgetError(t *testing.T) {
	driver, err := kit.Tool("lookup", func(_ context.Context, _ struct {
		Query string `json:"query"`
	}) (string, error) {
		return "result", nil
	})
	if err != nil {
		t.Fatalf("tool setup: %v", err)
	}
	engine := Engine{
		Provider: &erroringSecondTurnProvider{},
		Tools:    tool.NewBus(driver),
	}

	out, runErr := engine.RunMessages(context.Background(), LoopInput{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "go")},
		MaxIterations: 3,
		ToolMode:      tool.ModeSequential,
	})

	if runErr == nil {
		t.Fatal("expected the second-turn provider error to surface")
	}
	// user prompt + assistant tool call + tool result must all survive.
	if len(out.Messages) < 3 {
		t.Fatalf("preserved messages = %d, want >= 3 (the completed first turn)", len(out.Messages))
	}
	if len(out.Steps) < 1 {
		t.Fatalf("preserved steps = %d, want >= 1 (the completed first turn)", len(out.Steps))
	}
	if out.StopReason != provider.StopReasonError {
		t.Fatalf("StopReason = %s, want %s", out.StopReason, provider.StopReasonError)
	}
}

// TestRunMessagesPreservesTraceWhenSinkEmitFails pins the trace-preservation
// contract for the one failure mode that previously broke it: when a Sink is
// configured and Emit fails on the tool-result frame, appendToolResults must
// return the history accumulated so far rather than nil, so the prompt, the
// assistant tool call, and the tool result all survive on the returned
// LoopOutput. Before the fix appendToolResults returned nil on an Emit failure,
// clobbering current so the partial-trace error path preserved an empty history.
func TestRunMessagesPreservesTraceWhenSinkEmitFails(t *testing.T) {
	driver, err := kit.Tool("lookup", func(_ context.Context, _ struct {
		Query string `json:"query"`
	}) (string, error) {
		return "result", nil
	})
	if err != nil {
		t.Fatalf("tool setup: %v", err)
	}

	sinkErr := errors.New("sink delivery failed")
	failingSink := stream.SinkFunc(func(_ context.Context, frame stream.Frame) error {
		// Let the model turn stream cleanly; fail only on the tool result so the
		// failure lands inside appendToolResults, the path under test.
		if frame.Kind == stream.FrameToolResult {
			return sinkErr
		}
		return nil
	})

	engine := Engine{
		Provider: &erroringSecondTurnProvider{},
		Tools:    tool.NewBus(driver),
	}

	out, runErr := engine.RunMessages(context.Background(), LoopInput{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "go")},
		MaxIterations: 3,
		ToolMode:      tool.ModeSequential,
		Sink:          failingSink,
	})

	if runErr == nil {
		t.Fatal("expected the sink Emit failure to surface as the loop error")
	}
	if !errors.Is(runErr, sinkErr) {
		t.Fatalf("errors.Is(runErr, sinkErr) = false, runErr = %v", runErr)
	}
	// prompt + assistant tool call + tool result must all survive the failure.
	if len(out.Messages) < 3 {
		t.Fatalf("preserved messages = %d, want >= 3 (prompt, assistant tool call, tool result)", len(out.Messages))
	}
	if out.StopReason != provider.StopReasonError {
		t.Fatalf("StopReason = %s, want %s", out.StopReason, provider.StopReasonError)
	}
}

// TestRunFastExitsOnCancelledContext pins the loop-top context check: a context
// that is already cancelled ends the run before any model turn is issued, and
// the cancellation cause is preserved on the typed failure so errors.Is walks
// to context.Canceled across the boundary.
func TestRunFastExitsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	driver := singleTurnProvider("answer")
	engine := Engine{Provider: driver, Model: "test-model"}

	result := engine.Run(ctx, api.Task{Goal: "do the thing"}, OutputPolicy{})

	if result.Failure == nil {
		t.Fatal("expected a failure when the context is already cancelled")
	}
	if !errors.Is(result.Failure, context.Canceled) {
		t.Fatalf("errors.Is(Failure, context.Canceled) = false, Failure = %v", result.Failure)
	}
	if len(driver.requests) != 0 {
		t.Fatalf("provider was called %d time(s); the loop must fast-exit before any model turn", len(driver.requests))
	}
}

// TestRunMessagesPreservesCompletedTurnWhenCancelledAfterDone pins that a
// context cancellation landing AFTER the provider delivered its terminal
// EventDone (which already carries the StopReason) but before the stream
// returns io.EOF does not discard the already-complete turn. collect's pre-Recv
// context check exists to avoid blocking on a slow provider, but a response that
// has finished must not be turned into a failure solely because EOF has not been
// read yet. Here an OnEvent callback cancels the context the moment it observes
// EventDone, deterministically landing the cancellation in that window.
func TestRunMessagesPreservesCompletedTurnWhenCancelledAfterDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	driver := singleTurnProvider("done")
	engine := Engine{Provider: driver, Model: "test-model"}

	out, runErr := engine.RunMessages(ctx, LoopInput{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "go")},
		MaxIterations: 1,
		OnEvent: func(ev provider.Event) error {
			if ev.Kind == provider.EventDone {
				cancel()
			}
			return nil
		},
	})

	if runErr != nil {
		t.Fatalf("a turn completed via EventDone must not fail when the context is cancelled before EOF: %v", runErr)
	}
	if out.StopReason != provider.StopReasonComplete {
		t.Fatalf("StopReason = %s, want %s (the completed turn's terminal reason)", out.StopReason, provider.StopReasonComplete)
	}
	last := out.Messages[len(out.Messages)-1]
	if last.Text != "done" {
		t.Fatalf("assistant text = %q, want %q (the completed response must be preserved)", last.Text, "done")
	}
}
