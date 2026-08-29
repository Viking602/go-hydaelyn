package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/tool"
	"github.com/Viking602/venat/tool/kit"
)

// An unavailable tool is a factual tool_unavailable failure. Applications
// choose whether to retry, reroute, or stop.
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
		},
		) (string, error) {
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

			result := engine.Run(context.Background(), Request{Prompt: "use a tool"}, OutputPolicy{})

			if result.Failure == nil {
				t.Fatal("expected a failure for an unavailable tool")
			}
			if result.Failure.Kind != FailureKindToolUnavailable {
				t.Fatalf("Failure.Kind = %s, want %s", result.Failure.Kind, FailureKindToolUnavailable)
			}
			if result.Failure.Reason == "" {
				t.Fatal("tool_unavailable failure must retain its reason")
			}
		})
	}
}

// Guardrail retry exhaustion is a factual output_blocked failure with the
// typed cause preserved for application policy.
func TestEngineRunClassifiesGuardrailRetryExhaustionAsOutputBlocked(t *testing.T) {
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

	result := engine.Run(context.Background(), Request{Prompt: "do the thing"}, OutputPolicy{})

	if result.Failure == nil {
		t.Fatal("expected a failure when guardrail retries are exhausted")
	}
	if result.Failure.Kind != FailureKindOutputBlocked {
		t.Fatalf("Failure.Kind = %s, want output_blocked", result.Failure.Kind)
	}
	var retryLimit *OutputGuardrailRetryLimitExceededError
	if !errors.As(result.Failure, &retryLimit) {
		t.Fatalf("Failure cause = %v, want OutputGuardrailRetryLimitExceededError", result.Failure)
	}
}
