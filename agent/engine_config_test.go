package agent

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
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
	if result.Failure.Kind != FailureKindEngineError {
		t.Fatalf("Failure.Kind = %s, want engine_error", result.Failure.Kind)
	}
}
