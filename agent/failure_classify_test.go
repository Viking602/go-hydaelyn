package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/tool"
	"github.com/Viking602/venat/tool/kit"
)

// TestEngineRunClassifiesUnavailableToolAsToolUnavailable pins the contract
// from spec 03 §dispatch policy: a tool the bus cannot serve surfaces as
// FailureKindToolUnavailable — retryable and escalatable — so a scheduler can
// apply the documented "retry with backoff, then escalate" path rather than
// treating an unavailable tool as an opaque engine_error. It covers both a
// name absent from a present bus (tool.ErrToolNotFound) and a missing bus
// altogether (ErrToolBusMissing); both previously fell through to engine_error.
func TestEngineRunClassifiesUnavailableToolAsToolUnavailable(t *testing.T) {
	toolCallTurn := func(name string) [][]provider.Event {
		return [][]provider.Event{{
			{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{ID: "call-1", Name: name}},
			{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
		}}
	}
	lookupBus := func(t *testing.T) *tool.Bus {
		t.Helper()
		driver, err := kit.Tool("lookup", func(_ context.Context, _ struct {
			Query string `json:"query"`
		}) (string, error) {
			return "result", nil
		})
		if err != nil {
			t.Fatalf("tool setup: %v", err)
		}
		return tool.NewBus(driver)
	}

	tests := []struct {
		name string
		bus  *tool.Bus
		call string
	}{
		{name: "unknown tool name on a present bus", bus: lookupBus(t), call: "ghost"},
		{name: "no tool bus configured", bus: nil, call: "lookup"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := Engine{Provider: &scriptedProvider{turns: toolCallTurn(tt.call)}, Tools: tt.bus}

			result := engine.Run(context.Background(), api.Task{Goal: "use a tool"}, OutputPolicy{})

			if result.Failure == nil {
				t.Fatal("expected a failure for an unavailable tool")
			}
			if result.Failure.Kind != FailureKindToolUnavailable {
				t.Fatalf("Failure.Kind = %s, want %s", result.Failure.Kind, FailureKindToolUnavailable)
			}
			if !result.Failure.Retryable {
				t.Fatal("tool_unavailable must be retryable (retry with backoff)")
			}
			if !result.Failure.Escalatable {
				t.Fatal("tool_unavailable must be escalatable (then escalate)")
			}
		})
	}
}

// TestEngineRunClassifiesGuardrailRetryExhaustionAsUnsafeAction pins the other
// half of the guardrail-refusal contract (the block half lives in
// engine_config_test.go): when an output guardrail keeps asking to retry but
// the loop cannot satisfy it within MaxIterations, the run fails as
// FailureKindUnsafeAction — escalatable, not retryable — rather than collapsing
// into an opaque engine_error. The typed cause stays walkable across the
// boundary so a scheduler can inspect which guardrail withheld the output.
func TestEngineRunClassifiesGuardrailRetryExhaustionAsUnsafeAction(t *testing.T) {
	engine := Engine{
		Provider:   singleTurnProvider("draft answer"),
		Model:      "test-model",
		LoopPolicy: LoopPolicy{MaxIterations: 1},
		OutputGuardrails: []OutputGuardrail{
			NewOutputGuardrail("retry", func(_ context.Context, _ OutputGuardrailInput) (OutputGuardrailResult, error) {
				return RetryOutput(message.NewText(message.RoleUser, "please revise")), nil
			}),
		},
	}

	result := engine.Run(context.Background(), api.Task{Goal: "do the thing"}, OutputPolicy{})

	if result.Failure == nil {
		t.Fatal("expected a failure when guardrail retries are exhausted")
	}
	if result.Failure.Kind != FailureKindUnsafeAction {
		t.Fatalf("Failure.Kind = %s, want unsafe_action", result.Failure.Kind)
	}
	if !result.Failure.Escalatable || result.Failure.Retryable {
		t.Fatalf("Failure = %#v, want escalatable and not retryable", result.Failure)
	}
	var retryLimit *OutputGuardrailRetryLimitExceededError
	if !errors.As(result.Failure, &retryLimit) {
		t.Fatalf("Failure cause = %v, want OutputGuardrailRetryLimitExceededError", result.Failure)
	}
}
