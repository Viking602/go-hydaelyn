package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/tool"
	"github.com/Viking602/go-hydaelyn/tool/kit"
)

// usageToolProvider emits a non-terminal tool call every turn and reports a
// fixed token usage per turn, so a token budget accumulates deterministically.
type usageToolProvider struct{ perTurn provider.Usage }

func (*usageToolProvider) Metadata() provider.Metadata { return provider.Metadata{Name: "usage-tool"} }

func (p *usageToolProvider) Stream(_ context.Context, _ provider.Request) (provider.Stream, error) {
	return provider.NewSliceStream([]provider.Event{
		{
			Kind:     provider.EventToolCall,
			ToolCall: &message.ToolCall{ID: "call-1", Name: "lookup", Arguments: json.RawMessage(`{"query":"x"}`)},
		},
		{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse, Usage: p.perTurn},
	}), nil
}

func newLoopToolEngine(t *testing.T, prov provider.Driver) Engine {
	t.Helper()
	driver, err := kit.Tool("lookup", func(_ context.Context, _ struct {
		Query string `json:"query"`
	}) (string, error) {
		return "result", nil
	})
	if err != nil {
		t.Fatalf("tool setup: %v", err)
	}
	return Engine{Provider: prov, Tools: tool.NewBus(driver)}
}

func TestRunMessagesMaxToolCallsBudgetAbortsWithPartialTrace(t *testing.T) {
	engine := newLoopToolEngine(t, &alwaysToolProvider{})
	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model:        "test-model",
		Messages:     []message.Message{message.NewText(message.RoleUser, "loop")},
		MaxToolCalls: 2,
	})
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("err = %v, want ErrBudgetExhausted", err)
	}
	// Two calls are spent (turns 0 and 1); the pre-turn check before turn 2 trips.
	if output.ToolCallsUsed != 2 {
		t.Fatalf("ToolCallsUsed = %d, want 2", output.ToolCallsUsed)
	}
	if len(output.Steps) != 2 {
		t.Fatalf("len(Steps) = %d, want 2 partial steps", len(output.Steps))
	}
	if output.StopReason != provider.StopReasonAborted {
		t.Fatalf("StopReason = %q, want aborted", output.StopReason)
	}
}

// parallelToolProvider emits two tool calls in a single turn, modeling a model
// that requests parallel tool execution within one model response.
type parallelToolProvider struct{}

func (*parallelToolProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "parallel-tool"}
}

func (*parallelToolProvider) Stream(_ context.Context, _ provider.Request) (provider.Stream, error) {
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{ID: "call-1", Name: "lookup", Arguments: json.RawMessage(`{"query":"a"}`)}},
		{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{ID: "call-2", Name: "lookup", Arguments: json.RawMessage(`{"query":"b"}`)}},
		{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
	}), nil
}

func TestRunMessagesMaxToolCallsBudgetGatesParallelBatchBeforeDispatch(t *testing.T) {
	executed := 0
	driver, err := kit.Tool("lookup", func(_ context.Context, _ struct {
		Query string `json:"query"`
	}) (string, error) {
		executed++
		return "result", nil
	})
	if err != nil {
		t.Fatalf("tool setup: %v", err)
	}
	engine := Engine{Provider: &parallelToolProvider{}, Tools: tool.NewBus(driver)}

	// A single turn asks for two tool calls but the budget allows only one. The
	// loop must refuse to dispatch the batch rather than run both and discover
	// the overrun a turn later.
	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model:        "test-model",
		Messages:     []message.Message{message.NewText(message.RoleUser, "loop")},
		MaxToolCalls: 1,
	})
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("err = %v, want ErrBudgetExhausted", err)
	}
	// Neither side-effecting tool runs: the gate fires before ExecuteBatch.
	if executed != 0 {
		t.Fatalf("tool executed %d times, want 0 (batch gated before dispatch)", executed)
	}
	if output.ToolCallsUsed != 0 {
		t.Fatalf("ToolCallsUsed = %d, want 0 (no calls spent)", output.ToolCallsUsed)
	}
	if output.StopReason != provider.StopReasonAborted {
		t.Fatalf("StopReason = %q, want aborted", output.StopReason)
	}
	// The model turn ran, so it is recorded as one failed step.
	if len(output.Steps) != 1 {
		t.Fatalf("len(Steps) = %d, want 1", len(output.Steps))
	}
	if output.Steps[0].Decision != StepDecisionFail {
		t.Fatalf("Steps[0].Decision = %q, want fail", output.Steps[0].Decision)
	}
}

func TestRunMessagesMaxToolCallsBudgetAllowsBatchThatExactlyFills(t *testing.T) {
	executed := 0
	driver, err := kit.Tool("lookup", func(_ context.Context, _ struct {
		Query string `json:"query"`
	}) (string, error) {
		executed++
		return "result", nil
	})
	if err != nil {
		t.Fatalf("tool setup: %v", err)
	}
	engine := Engine{Provider: &parallelToolProvider{}, Tools: tool.NewBus(driver)}

	// Two calls with a budget of exactly two: the batch fits, both tools run,
	// and the run aborts only on the next pre-turn check.
	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model:        "test-model",
		Messages:     []message.Message{message.NewText(message.RoleUser, "loop")},
		MaxToolCalls: 2,
	})
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("err = %v, want ErrBudgetExhausted on the following turn", err)
	}
	if executed != 2 {
		t.Fatalf("tool executed %d times, want 2 (batch exactly fills the budget)", executed)
	}
	if output.ToolCallsUsed != 2 {
		t.Fatalf("ToolCallsUsed = %d, want 2", output.ToolCallsUsed)
	}
}

func TestRunMessagesMaxStepsBudgetIsHardFailure(t *testing.T) {
	engine := newLoopToolEngine(t, &alwaysToolProvider{})
	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model:    "test-model",
		Messages: []message.Message{message.NewText(message.RoleUser, "loop")},
		MaxSteps: 3, // below the default soft ceiling of 12 iterations
	})
	// Unlike MaxIterations (a soft ceiling -> StopReasonMaxTurns, no error),
	// MaxSteps is a hard budget: exhausting it fails the run.
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("err = %v, want ErrBudgetExhausted (MaxSteps is a hard ceiling)", err)
	}
	if len(output.Steps) != 3 {
		t.Fatalf("len(Steps) = %d, want exactly 3", len(output.Steps))
	}
}

func TestRunMessagesMaxTokensBudgetFailsOpenOnZeroUsageProvider(t *testing.T) {
	// alwaysToolProvider reports no usage; a token ceiling must never trip on
	// it, so the loop runs to its soft iteration ceiling instead of failing.
	engine := newLoopToolEngine(t, &alwaysToolProvider{})
	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "loop")},
		MaxTokens:     5,
		MaxIterations: 3,
	})
	if err != nil {
		t.Fatalf("token budget must fail open on a zero-usage provider, got %v", err)
	}
	if output.StopReason != provider.StopReasonMaxTurns {
		t.Fatalf("StopReason = %q, want max-turns (soft ceiling, not a budget failure)", output.StopReason)
	}
	if len(output.Steps) != 3 {
		t.Fatalf("len(Steps) = %d, want 3", len(output.Steps))
	}
}

func TestRunMessagesMaxTokensBudgetAbortsWhenUsageReported(t *testing.T) {
	engine := newLoopToolEngine(t, &usageToolProvider{perTurn: provider.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5}})
	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model:     "test-model",
		Messages:  []message.Message{message.NewText(message.RoleUser, "loop")},
		MaxTokens: 8,
	})
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("err = %v, want ErrBudgetExhausted", err)
	}
	// Turn 0 spends 5 (<8, continues); turn 1 brings the total to 10 (>=8), so
	// the pre-turn check before turn 2 trips.
	if len(output.Steps) != 2 {
		t.Fatalf("len(Steps) = %d, want 2", len(output.Steps))
	}
	if output.Usage.TotalTokens != 10 {
		t.Fatalf("Usage.TotalTokens = %d, want 10", output.Usage.TotalTokens)
	}
}

func TestRunMessagesBudgetNotChargedWhenRunFinishes(t *testing.T) {
	// A single finishing turn is never failed for budget: the ceilings guard
	// continuation, and the first turn always runs. Even though this turn
	// reports usage far over every ceiling, the run completes cleanly.
	driver := &scriptedProvider{turns: [][]provider.Event{{
		{Kind: provider.EventTextDelta, Text: "done"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete, Usage: provider.Usage{TotalTokens: 1000}},
	}}}
	engine := Engine{Provider: driver}
	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "hi")},
		MaxTokens:     5,
		MaxSteps:      1,
		MaxToolCalls:  1,
		MaxIterations: 3,
	})
	if err != nil {
		t.Fatalf("finishing run must not be budget-failed, got %v", err)
	}
	if output.StopReason != provider.StopReasonComplete {
		t.Fatalf("StopReason = %q, want complete", output.StopReason)
	}
	if len(output.Steps) != 1 {
		t.Fatalf("len(Steps) = %d, want 1", len(output.Steps))
	}
}

func TestEngineRunMapsBudgetExhaustionToFailure(t *testing.T) {
	engine := newLoopToolEngine(t, &alwaysToolProvider{})
	engine.LoopPolicy = LoopPolicy{Budget: &api.TaskBudget{MaxToolCalls: 2}}

	result := engine.Run(context.Background(), api.Task{Goal: "loop"}, OutputPolicy{})

	if result.Failure == nil || result.Failure.Kind != FailureKindBudgetExhausted {
		t.Fatalf("Failure = %#v, want budget_exhausted", result.Failure)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("len(Steps) = %d, want 2 partial steps on the failed result", len(result.Steps))
	}
}

func TestEngineRunPerTaskBudgetOverridesLoopPolicy(t *testing.T) {
	engine := newLoopToolEngine(t, &alwaysToolProvider{})
	// The engine default would allow five tool calls; the task tightens it to one.
	engine.LoopPolicy = LoopPolicy{Budget: &api.TaskBudget{MaxToolCalls: 5}}

	result := engine.Run(context.Background(), api.Task{
		Goal:   "loop",
		Budget: &api.TaskBudget{MaxToolCalls: 1},
	}, OutputPolicy{})

	if result.Failure == nil || result.Failure.Kind != FailureKindBudgetExhausted {
		t.Fatalf("Failure = %#v, want budget_exhausted", result.Failure)
	}
	if len(result.Steps) != 1 {
		t.Fatalf("len(Steps) = %d, want 1 (task budget of one tool call wins)", len(result.Steps))
	}
}

func TestEngineRunTaskBudgetIsAuthoritativeAndDoesNotInheritEngineCaps(t *testing.T) {
	engine := newLoopToolEngine(t, &alwaysToolProvider{})
	// The engine default caps tool calls at 2 and allows up to 5 iterations.
	// The task supplies its own Budget that bounds only tokens, leaving
	// MaxToolCalls zero — which the api.TaskBudget contract defines as unbounded.
	engine.LoopPolicy = LoopPolicy{MaxIterations: 5, Budget: &api.TaskBudget{MaxToolCalls: 2}}

	result := engine.Run(context.Background(), api.Task{
		Goal:   "loop",
		Budget: &api.TaskBudget{MaxTokens: 1000},
	}, OutputPolicy{})

	// A present task budget is authoritative: its zero MaxToolCalls means
	// unbounded, so the engine's cap of 2 must NOT be inherited (under the old
	// per-dimension merge this run failed with budget_exhausted after 2 calls).
	// The token ceiling fails open on the zero-usage provider, so the run
	// reaches the soft iteration ceiling instead of a budget failure.
	if result.Failure != nil {
		t.Fatalf("task budget must not inherit the engine tool-call cap; got failure %#v", result.Failure)
	}
	if result.StopReason != provider.StopReasonMaxTurns {
		t.Fatalf("StopReason = %q, want max-turns", result.StopReason)
	}
	if len(result.Steps) != 5 {
		t.Fatalf("len(Steps) = %d, want 5 (ran to the iteration ceiling, tool calls unbounded)", len(result.Steps))
	}
}

func TestEngineRepairStopsWhenBudgetExhaustedBeforeAttempt(t *testing.T) {
	// A MaxSteps budget of 1: the initial run spends the only step, so the
	// repair pre-check fails before issuing a second model call.
	driver, engine := newOutputPolicyEngine(
		`{"status":"blocked","score":0.75,"count":2,"tags":["risk"],"accepted":true}`,
		`{"status":"ok","score":0.75,"count":2,"tags":["risk"],"accepted":true}`,
	)
	engine.LoopPolicy = LoopPolicy{Budget: &api.TaskBudget{MaxSteps: 1}}
	policy := outputPolicyReport()
	policy.Repair = true
	policy.MaxRepairAttempts = 2

	result := engine.Run(context.Background(), api.Task{Goal: "classify risk"}, policy)

	if result.Failure == nil || result.Failure.Kind != FailureKindBudgetExhausted {
		t.Fatalf("Failure = %#v, want budget_exhausted before repair", result.Failure)
	}
	if result.RepairCount != 0 {
		t.Fatalf("RepairCount = %d, want 0 (no repair attempt ran)", result.RepairCount)
	}
	if len(driver.requests) != 1 {
		t.Fatalf("provider calls = %d, want 1 (repair never issued)", len(driver.requests))
	}
}
