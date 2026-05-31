package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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
	// The tool call was dispatched by executeTools before the sink failed on its
	// result frame, so the partial output must charge it: a caller resuming from
	// this LoopOutput would otherwise under-count its MaxToolCalls budget.
	if out.ToolCallsUsed < 1 {
		t.Fatalf("ToolCallsUsed = %d, want >= 1 (the dispatched tool call must be charged on the partial output)", out.ToolCallsUsed)
	}
}

// ctxAwareStream models a real provider stream — anthropic and openai wrap an
// HTTP body — whose Recv unblocks via the request context rather than io.EOF.
// It replays its queued events in order; once they are drained it surfaces a
// cancelled context as ctx.Err() (the way a body read does) instead of io.EOF.
// This is the one termination style SliceStream cannot model, because
// SliceStream ignores the context and always ends a drained stream with io.EOF.
type ctxAwareStream struct {
	ctx    context.Context
	events []provider.Event
	pos    int
}

func (s *ctxAwareStream) Recv() (provider.Event, error) {
	if s.pos < len(s.events) {
		event := s.events[s.pos]
		s.pos++
		return event, nil
	}
	if err := s.ctx.Err(); err != nil {
		return provider.Event{}, err
	}
	return provider.Event{}, io.EOF
}

func (*ctxAwareStream) Close() error { return nil }

// ctxAwareProvider hands collect a ctxAwareStream wired to the request context,
// so a cancellation that lands after the terminal EventDone reaches Recv as
// ctx.Err() rather than io.EOF.
type ctxAwareProvider struct{ events []provider.Event }

func (ctxAwareProvider) Metadata() provider.Metadata { return provider.Metadata{Name: "ctx-aware"} }

func (p ctxAwareProvider) Stream(ctx context.Context, _ provider.Request) (provider.Stream, error) {
	return &ctxAwareStream{ctx: ctx, events: p.events}, nil
}

// TestRunMessagesPreservesCompletedTurnWhenStreamUnblocksViaContext pins the
// companion case to the io.EOF one: when a context-aware provider stream has
// already delivered its terminal EventDone and the context is then cancelled,
// the next Recv returns context.Canceled rather than io.EOF. collect must treat
// that as the end of an already-complete turn and normalize the events it holds,
// not discard the turn as a failure. The bundled anthropic/openai streams
// short-circuit to io.EOF after EventDone so they never hit this path, but the
// public provider.Stream contract permits a stream that unblocks Recv through
// the context, and the trace-preservation guarantee must hold for it too.
func TestRunMessagesPreservesCompletedTurnWhenStreamUnblocksViaContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	driver := ctxAwareProvider{events: []provider.Event{
		{Kind: provider.EventTextDelta, Text: "done"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}}
	engine := Engine{Provider: driver, Model: "test-model"}

	out, runErr := engine.RunMessages(ctx, LoopInput{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "go")},
		MaxIterations: 1,
		OnEvent: func(ev provider.Event) error {
			// Cancel the moment the terminal event is observed so the NEXT Recv,
			// on this context-aware stream, returns context.Canceled not io.EOF.
			if ev.Kind == provider.EventDone {
				cancel()
			}
			return nil
		},
	})

	if runErr != nil {
		t.Fatalf("a turn completed via EventDone must survive a context-cancelled Recv, not only an io.EOF one: %v", runErr)
	}
	if out.StopReason != provider.StopReasonComplete {
		t.Fatalf("StopReason = %s, want %s (the completed turn's terminal reason)", out.StopReason, provider.StopReasonComplete)
	}
	last := out.Messages[len(out.Messages)-1]
	if last.Text != "done" {
		t.Fatalf("assistant text = %q, want %q (the completed response must be preserved)", last.Text, "done")
	}
}

// panicToolDriver panics from Execute. Run sequentially it executes inline on
// the loop's goroutine, so its panic propagates to RunMessages' own recover
// (parallel drivers are contained by the bus instead). It models a misbehaving
// tool driver that crashes after a model turn has already run.
type panicToolDriver struct{}

func (panicToolDriver) Definition() tool.Definition {
	return tool.Definition{Name: "boom"}
}

func (panicToolDriver) Execute(_ context.Context, _ tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	panic("tool driver exploded")
}

// TestRunMessagesReportsIterationWhenToolDriverPanics pins that the panic
// backstop reports Iterations from the turns that actually ran, not from the
// completed-Step count. A sequential tool driver panics after the model turn has
// run and its assistant tool-call message is already in the partial trace, but
// before this turn's Step is recorded. Deriving Iterations from len(steps) would
// report 0 while the partial output carries the turn's messages — an
// inconsistency for a direct RunMessages consumer. Iterations must be >= 1.
func TestRunMessagesReportsIterationWhenToolDriverPanics(t *testing.T) {
	driver := &scriptedProvider{turns: [][]provider.Event{{
		{
			Kind: provider.EventToolCall,
			ToolCall: &message.ToolCall{
				ID:        "call-1",
				Name:      "boom",
				Arguments: json.RawMessage(`{}`),
			},
		},
		{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
	}}}
	engine := Engine{
		Provider: driver,
		Model:    "test-model",
		Tools:    tool.NewBus(panicToolDriver{}),
	}

	out, runErr := engine.RunMessages(context.Background(), LoopInput{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "go")},
		MaxIterations: 2,
		ToolMode:      tool.ModeSequential,
	})

	if runErr == nil || !errors.Is(runErr, ErrPanicRecovered) {
		t.Fatalf("expected ErrPanicRecovered from the panicking tool driver, got %v", runErr)
	}
	// The assistant tool-call message is preserved, so reporting Iterations=0
	// would contradict the partial trace.
	if len(out.Messages) < 2 {
		t.Fatalf("preserved messages = %d, want >= 2 (prompt + assistant tool call)", len(out.Messages))
	}
	if out.Iterations < 1 {
		t.Fatalf("Iterations = %d, want >= 1 (the model turn ran before the tool-driver panic)", out.Iterations)
	}
}

// recordingDriver records whether Execute was invoked. It deliberately does
// not consult the context, modeling a side-effecting tool whose execution
// under a cancelled context must be prevented by the loop rather than left to
// the driver — the tool bus dispatches without a pre-flight context check.
type recordingDriver struct{ invoked bool }

func (*recordingDriver) Definition() tool.Definition {
	return tool.Definition{Name: "side_effect"}
}

func (d *recordingDriver) Execute(_ context.Context, _ tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	d.invoked = true
	return tool.Result{ToolCallID: "call-1", Name: "side_effect", Content: "ran"}, nil
}

// TestRunMessagesDoesNotDispatchToolsWhenCancelledAfterDone pins the boundary of
// the completed-turn preservation: a turn whose terminal EventDone asks for
// tools must NOT have those tools dispatched when the run context is already
// cancelled. Preserving a completed final-answer turn under cancellation is
// correct — the response is finished — but a tool-use turn is a request to
// perform side-effecting work, and the bus dispatches to drivers without a
// pre-flight context check, so the loop must refuse dispatch once the context is
// done. Here OnEvent cancels the moment EventDone (StopReasonToolUse) is seen;
// the recording driver must never run, and the assistant tool-call request must
// still be preserved in the trace.
func TestRunMessagesDoesNotDispatchToolsWhenCancelledAfterDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	driver := &scriptedProvider{turns: [][]provider.Event{{
		{
			Kind: provider.EventToolCall,
			ToolCall: &message.ToolCall{
				ID:        "call-1",
				Name:      "side_effect",
				Arguments: json.RawMessage(`{}`),
			},
		},
		{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
	}}}
	sideEffect := &recordingDriver{}
	engine := Engine{
		Provider: driver,
		Model:    "test-model",
		Tools:    tool.NewBus(sideEffect),
	}

	out, runErr := engine.RunMessages(ctx, LoopInput{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "go")},
		MaxIterations: 2,
		ToolMode:      tool.ModeSequential,
		OnEvent: func(ev provider.Event) error {
			// Cancel the instant the tool-use turn completes, landing the
			// cancellation in the window before the loop dispatches its tools.
			if ev.Kind == provider.EventDone {
				cancel()
			}
			return nil
		},
	})

	if sideEffect.invoked {
		t.Fatal("a side-effecting tool must not be dispatched when the run context is cancelled before dispatch")
	}
	if runErr == nil {
		t.Fatal("expected the cancellation to surface as the loop error")
	}
	if !errors.Is(runErr, context.Canceled) {
		t.Fatalf("errors.Is(runErr, context.Canceled) = false, runErr = %v", runErr)
	}
	if out.StopReason != provider.StopReasonError {
		t.Fatalf("StopReason = %s, want %s", out.StopReason, provider.StopReasonError)
	}
	// The assistant tool-call request must survive: prompt + assistant tool call.
	if len(out.Messages) < 2 {
		t.Fatalf("preserved messages = %d, want >= 2 (prompt + the assistant tool-call request)", len(out.Messages))
	}
	if last := out.Messages[len(out.Messages)-1]; len(last.ToolCalls) == 0 {
		t.Fatal("the assistant tool-call request must be preserved in the trace")
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

// erroringToolDriver returns an error from Execute rather than a result. Run
// sequentially through the bus it makes ExecuteBatch return that error, so
// executeTools surfaces it and the loop takes the toolErr partial-error return —
// the path that previously left the dispatched call uncharged. It models a tool
// that runs and fails (a remote 500, a validation error) after a model turn has
// already requested it.
type erroringToolDriver struct{}

func (erroringToolDriver) Definition() tool.Definition {
	return tool.Definition{Name: "explode"}
}

func (erroringToolDriver) Execute(_ context.Context, _ tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	return tool.Result{}, errors.New("tool driver failed")
}

// TestRunMessagesChargesToolBatchWhenDriverErrors pins that a tool-execution
// error charges this turn's dispatched batch against ToolCallsUsed before the
// partial-error return. The bus dispatched the call before the driver failed, so
// a caller that persists or resumes from the returned partial LoopOutput must
// see it counted — otherwise the failed call escapes MaxToolCalls and later work
// can overrun the budget. The increment previously sat only on the success path
// below the toolErr check, so this output reported ToolCallsUsed = 0; the loop
// now charges the batch as soon as dispatch is attempted, mirroring the
// batch-level reservation the pre-dispatch budget gate already makes.
func TestRunMessagesChargesToolBatchWhenDriverErrors(t *testing.T) {
	driver := &scriptedProvider{turns: [][]provider.Event{{
		{
			Kind: provider.EventToolCall,
			ToolCall: &message.ToolCall{
				ID:        "call-1",
				Name:      "explode",
				Arguments: json.RawMessage(`{}`),
			},
		},
		{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
	}}}
	engine := Engine{
		Provider: driver,
		Model:    "test-model",
		Tools:    tool.NewBus(erroringToolDriver{}),
	}

	out, runErr := engine.RunMessages(context.Background(), LoopInput{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "go")},
		MaxIterations: 2,
		ToolMode:      tool.ModeSequential,
	})

	if runErr == nil {
		t.Fatal("expected the tool driver error to surface as the loop error")
	}
	if out.StopReason != provider.StopReasonError {
		t.Fatalf("StopReason = %s, want %s", out.StopReason, provider.StopReasonError)
	}
	// The bus dispatched the call before the driver returned its error, so the
	// partial output must charge it against MaxToolCalls; reporting 0 would let a
	// resuming caller exceed its tool-call budget.
	if out.ToolCallsUsed < 1 {
		t.Fatalf("ToolCallsUsed = %d, want >= 1 (the dispatched tool call must be charged on the toolErr partial output)", out.ToolCallsUsed)
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
