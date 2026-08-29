package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/tool"
)

type recordingGuardrailObserver struct {
	decisions []OutputGuardrailDecision
}

func (o *recordingGuardrailObserver) ObserveOutputGuardrailDecision(_ context.Context, decision OutputGuardrailDecision) {
	o.decisions = append(o.decisions, decision)
}

type mutatingProviderRequestHook struct{}

func (mutatingProviderRequestHook) TransformContext(_ context.Context, messages []message.Message) ([]message.Message, error) {
	return messages, nil
}

func (mutatingProviderRequestHook) BeforeModelCall(_ context.Context, request *provider.Request) error {
	request.Metadata["scope"] = "hook"
	request.StopSequences[0] = "HOOK"
	request.ResponseFormat.Schema.Required[0] = "hook"
	request.ExtraBody["nested"].(map[string]any)["enabled"] = false
	return nil
}

func (mutatingProviderRequestHook) BeforeToolCall(context.Context, *tool.Call) error {
	return nil
}

func (mutatingProviderRequestHook) AfterToolCall(context.Context, *tool.Result) error {
	return nil
}

func (mutatingProviderRequestHook) OnEvent(context.Context, provider.Event) error {
	return nil
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

	result := engine.Run(context.Background(), Request{Prompt: "do the thing"}, OutputPolicy{})

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

	engine.Run(context.Background(), Request{Prompt: "do the thing"}, OutputPolicy{})

	if len(driver.requests) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(driver.requests))
	}
	if got := driver.requests[0].ExtraBody["reasoning_effort"]; got != "high" {
		t.Fatalf("ExtraBody[reasoning_effort] = %v, want high", got)
	}
}

func TestRunMessages_ModelHookDoesNotAliasInputDefaults(t *testing.T) {
	driver := singleTurnProvider("answer")
	metadata := map[string]string{"scope": "caller"}
	stopSequences := []string{"STOP"}
	responseFormat := &provider.ResponseFormat{
		Type: "json_schema",
		Schema: &message.JSONSchema{
			Type:     "object",
			Required: []string{"answer"},
		},
	}
	nested := map[string]any{"enabled": true}
	extraBody := map[string]any{"nested": nested}
	engine := Engine{
		Provider: driver,
		Hooks:    NewHookChain(mutatingProviderRequestHook{}),
	}

	_, err := engine.RunMessages(context.Background(), LoopInput{
		Model:          "test-model",
		Messages:       []message.Message{message.NewText(message.RoleUser, "work")},
		MaxIterations:  1,
		Metadata:       metadata,
		StopSequences:  stopSequences,
		ResponseFormat: responseFormat,
		ExtraBody:      extraBody,
	})
	if err != nil {
		t.Fatalf("RunMessages() error = %v", err)
	}
	if metadata["scope"] != "caller" || stopSequences[0] != "STOP" {
		t.Fatalf("hook mutated caller metadata/stops: %#v %#v", metadata, stopSequences)
	}
	if responseFormat.Schema.Required[0] != "answer" {
		t.Fatalf("hook mutated caller response schema: %#v", responseFormat.Schema)
	}
	if nested["enabled"] != true {
		t.Fatalf("hook mutated caller ExtraBody: %#v", extraBody)
	}
	request := driver.requests[0]
	if request.Metadata["scope"] != "hook" || request.StopSequences[0] != "HOOK" {
		t.Fatalf("provider request did not receive hook rewrite: %#v", request)
	}
	if request.ResponseFormat.Schema.Required[0] != "hook" {
		t.Fatalf("provider response schema = %#v, want hook rewrite", request.ResponseFormat.Schema)
	}
	if request.ExtraBody["nested"].(map[string]any)["enabled"] != false {
		t.Fatalf("provider ExtraBody = %#v, want hook rewrite", request.ExtraBody)
	}
}

func TestEngineRunAppliesOutputGuardrailsAndRecorder(t *testing.T) {
	driver := singleTurnProvider("raw answer")
	observer := &recordingGuardrailObserver{}
	engine := Engine{
		Provider:       driver,
		Model:          "test-model",
		OutputObserver: observer,
		OutputGuardrails: []OutputGuardrail{
			NewOutputGuardrail("redact", func(_ context.Context, in OutputGuardrailInput) (OutputGuardrailResult, error) {
				if in.Output.Text == "raw answer" {
					return ReplaceOutput(message.NewText(message.RoleAssistant, "clean answer")), nil
				}
				return AllowOutput(), nil
			}),
		},
	}

	result := engine.Run(context.Background(), Request{Prompt: "do the thing"}, OutputPolicy{})

	if result.Failure != nil {
		t.Fatalf("unexpected failure: %#v", result.Failure)
	}
	if result.Text != "clean answer" {
		t.Fatalf("result.Text = %q, want the guardrail replacement", result.Text)
	}
	if len(observer.decisions) != 1 {
		t.Fatalf("observed decisions = %d, want 1", len(observer.decisions))
	}
	if decision := observer.decisions[0]; decision.GuardrailName != "redact" || decision.Action != OutputGuardrailActionReplace {
		t.Fatalf("decision = %#v, want redact/replace", decision)
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

	result := engine.Run(context.Background(), Request{Prompt: "do the thing"}, OutputPolicy{})

	if result.Failure == nil {
		t.Fatal("expected a failure when a guardrail blocks the output")
	}
	if result.Failure.Kind != FailureKindOutputBlocked {
		t.Fatalf("Failure.Kind = %s, want output_blocked", result.Failure.Kind)
	}
	var tripwire *OutputGuardrailTripwireTriggeredError
	if !errors.As(result.Failure, &tripwire) {
		t.Fatalf("Failure cause = %v, want OutputGuardrailTripwireTriggeredError", result.Failure)
	}
}

// failingContextManager always fails Build, pinning the engine's
// failure-classification of context construction errors.
type failingContextManager struct{}

func (failingContextManager) Build(context.Context, Request) ([]message.Message, error) {
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

	result := engine.Run(context.Background(), Request{Prompt: "g"}, OutputPolicy{})

	if result.Failure == nil || result.Failure.Kind != FailureKindContextBuildFailed {
		t.Fatalf("Failure = %#v, want Kind=context_build_failed", result.Failure)
	}
}
