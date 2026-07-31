package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
)

// recordingRecorder captures every guardrail decision the engine reports, so
// tests can assert the OutputRecorder is wired through Engine.Run.
type recordingRecorder struct {
	decisions []OutputGuardrailDecision
}

func (r *recordingRecorder) RecordOutputGuardrailDecision(_ context.Context, decision OutputGuardrailDecision) {
	r.decisions = append(r.decisions, decision)
}

func singleTurnProvider(text string) *scriptedProvider {
	return &scriptedProvider{turns: [][]provider.Event{{
		{Kind: provider.EventTextDelta, Text: text},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}}}
}

func TestEngineRunWiresThinkingBudgetAndStopSequencesToProvider(t *testing.T) {
	driver := singleTurnProvider("answer")
	engine := Engine{
		Provider:       driver,
		Model:          "test-model",
		ThinkingBudget: 512,
		StopSequences:  []string{"STOP", "END"},
	}

	result := engine.Run(context.Background(), api.Task{Goal: "do the thing"}, OutputPolicy{})

	if result.Failure != nil {
		t.Fatalf("unexpected failure: %#v", result.Failure)
	}
	if len(driver.requests) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(driver.requests))
	}
	req := driver.requests[0]
	if req.ThinkingBudget != 512 {
		t.Fatalf("ThinkingBudget = %d, want 512", req.ThinkingBudget)
	}
	if len(req.StopSequences) != 2 || req.StopSequences[0] != "STOP" || req.StopSequences[1] != "END" {
		t.Fatalf("StopSequences = %#v, want [STOP END]", req.StopSequences)
	}
}

func TestEngineRunWiresExtraBodyToProvider(t *testing.T) {
	driver := singleTurnProvider("answer")
	engine := Engine{
		Provider:  driver,
		Model:     "test-model",
		ExtraBody: map[string]any{"reasoning_effort": "high"},
	}

	engine.Run(context.Background(), api.Task{Goal: "do the thing"}, OutputPolicy{})

	if len(driver.requests) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(driver.requests))
	}
	if got := driver.requests[0].ExtraBody["reasoning_effort"]; got != "high" {
		t.Fatalf("ExtraBody[reasoning_effort] = %v, want high", got)
	}
}

func TestEngineRunAppliesOutputGuardrailsAndRecorder(t *testing.T) {
	driver := singleTurnProvider("raw answer")
	recorder := &recordingRecorder{}
	engine := Engine{
		Provider:       driver,
		Model:          "test-model",
		OutputRecorder: recorder,
		OutputGuardrails: []OutputGuardrail{
			NewOutputGuardrail("redact", func(_ context.Context, in OutputGuardrailInput) (OutputGuardrailResult, error) {
				if in.Output.Text == "raw answer" {
					return ReplaceOutput(message.NewText(message.RoleAssistant, "clean answer")), nil
				}
				return AllowOutput(), nil
			}),
		},
	}

	result := engine.Run(context.Background(), api.Task{Goal: "do the thing"}, OutputPolicy{})

	if result.Failure != nil {
		t.Fatalf("unexpected failure: %#v", result.Failure)
	}
	if result.Text != "clean answer" {
		t.Fatalf("result.Text = %q, want the guardrail replacement", result.Text)
	}
	if len(recorder.decisions) != 1 {
		t.Fatalf("recorded decisions = %d, want 1", len(recorder.decisions))
	}
	if d := recorder.decisions[0]; d.GuardrailName != "redact" || d.Action != OutputGuardrailActionReplace {
		t.Fatalf("decision = %#v, want redact/replace", d)
	}
}

func TestEngineRunBlockingGuardrailFailsRun(t *testing.T) {
	driver := singleTurnProvider("unsafe answer")
	engine := Engine{
		Provider: driver,
		Model:    "test-model",
		OutputGuardrails: []OutputGuardrail{
			NewOutputGuardrail("block", func(_ context.Context, _ OutputGuardrailInput) (OutputGuardrailResult, error) {
				return BlockOutput("unsafe"), nil
			}),
		},
	}

	result := engine.Run(context.Background(), api.Task{Goal: "do the thing"}, OutputPolicy{})

	if result.Failure == nil {
		t.Fatal("expected a failure when a guardrail blocks the output")
	}
	// A guardrail block is a safety refusal, not an opaque engine fault: it
	// surfaces as unsafe_action so a scheduler escalates for human review
	// rather than blindly retrying. The typed cause stays on the chain.
	if result.Failure.Kind != FailureKindUnsafeAction {
		t.Fatalf("Failure.Kind = %s, want unsafe_action", result.Failure.Kind)
	}
	if !result.Failure.Escalatable || result.Failure.Retryable {
		t.Fatalf("Failure = %#v, want escalatable and not retryable", result.Failure)
	}
	var tripwire *OutputGuardrailTripwireTriggeredError
	if !errors.As(result.Failure, &tripwire) {
		t.Fatalf("Failure cause = %v, want OutputGuardrailTripwireTriggeredError", result.Failure)
	}
}

// failingContextManager always fails Build, pinning the engine's
// failure-classification of context construction errors.
type failingContextManager struct{}

func (failingContextManager) Build(context.Context, api.Task) ([]message.Message, error) {
	return nil, errors.New("boom: context source unavailable")
}

func (failingContextManager) Compact(_ context.Context, history []message.Message) ([]message.Message, error) {
	return history, nil
}

func TestRun_ContextBuildErrorMapsToContextBuildFailed(t *testing.T) {
	engine := Engine{Provider: &scriptedProvider{turns: [][]provider.Event{{
		{Kind: provider.EventTextDelta, Text: "never reached"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}}}}
	engine.ContextBuilder = failingContextManager{}

	result := engine.Run(context.Background(), api.Task{Goal: "g"}, OutputPolicy{})

	if result.Failure == nil || result.Failure.Kind != FailureKindContextBuildFailed {
		t.Fatalf("Failure = %#v, want Kind=context_build_failed", result.Failure)
	}
}
