package host

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/capability"
	"github.com/Viking602/go-hydaelyn/hook"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/internal/middleware"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/session"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/tool"
	"github.com/Viking602/go-hydaelyn/toolkit"
)

type orderedExtensionProvider struct {
	turn int
}

func (p *orderedExtensionProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "ordered-extension"}
}

func (p *orderedExtensionProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	p.turn++
	if p.turn == 1 {
		return provider.NewSliceStream([]provider.Event{
			{
				Kind: provider.EventToolCall,
				ToolCall: &message.ToolCall{
					ID:        "call-1",
					Name:      "trace-tool",
					Arguments: json.RawMessage(`{"topic":"middleware-order"}`),
				},
			},
			{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
		}), nil
	}
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "final answer"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

type traceHook struct {
	trace *[]string
}

func (h traceHook) TransformContext(_ context.Context, messages []message.Message) ([]message.Message, error) {
	*h.trace = append(*h.trace, "hook.transform_context")
	return messages, nil
}

func (h traceHook) BeforeModelCall(_ context.Context, _ *provider.Request) error {
	*h.trace = append(*h.trace, "hook.before_model_call")
	return nil
}

func (h traceHook) BeforeToolCall(_ context.Context, _ *tool.Call) error {
	*h.trace = append(*h.trace, "hook.before_tool_call")
	return nil
}

func (h traceHook) AfterToolCall(_ context.Context, _ *tool.Result) error {
	*h.trace = append(*h.trace, "hook.after_tool_call")
	return nil
}

func (h traceHook) OnEvent(_ context.Context, _ provider.Event) error {
	*h.trace = append(*h.trace, "hook.event")
	return nil
}

var _ hook.Handler = traceHook{}

func TestUseStageMiddlewareDelegatesToPromptLifecycle(t *testing.T) {
	runner := New(Config{})
	providerDriver := &orderedExtensionProvider{}
	runner.RegisterProvider("ordered-extension", providerDriver)
	driver, err := toolkit.Tool("trace-tool", func(_ context.Context, input struct {
		Topic string `json:"topic"`
	}) (string, error) {
		return "topic:" + input.Topic, nil
	})
	if err != nil {
		t.Fatalf("Tool() error = %v", err)
	}
	runner.RegisterTool(driver)

	trace := make([]string, 0, 4)
	runner.UseStageMiddleware(middleware.Func(func(ctx context.Context, envelope *middleware.Envelope, next middleware.Next) error {
		trace = append(trace, string(envelope.Stage)+":"+envelope.Operation)
		return next(ctx, envelope)
	}))
	sess, err := runner.CreateSession(context.Background(), session.CreateParams{Branch: "main"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, err := runner.Prompt(context.Background(), PromptRequest{
		SessionID: sess.ID,
		Provider:  "ordered-extension",
		Model:     "test",
		Messages:  []message.Message{message.NewText(message.RoleUser, "go")},
	}); err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}

	required := map[string]bool{
		"agent:prompt":          false,
		"llm:transform_context": false,
		"llm:before":            false,
		"llm:event":             false,
		"tool:before":           false,
		"tool:after":            false,
	}
	for _, item := range trace {
		if _, ok := required[item]; ok {
			required[item] = true
		}
	}
	for item, ok := range required {
		if !ok {
			t.Fatalf("expected stage trace %q in %#v", item, trace)
		}
	}
}

func TestUseCapabilityPolicyDelegatesToCapabilityInvoker(t *testing.T) {
	runner := New(Config{})
	trace := make([]string, 0, 2)
	runner.UseCapabilityPolicy(capability.Func(func(ctx context.Context, call capability.Call, next capability.Next) (capability.Result, error) {
		trace = append(trace, string(call.Type)+":"+call.Name)
		return next(ctx, call)
	}))
	runner.RegisterCapability(capability.TypeSearch, "web", func(context.Context, capability.Call) (capability.Result, error) {
		return capability.Result{Output: "ok"}, nil
	})

	if _, err := runner.InvokeCapability(context.Background(), capability.Call{Type: capability.TypeSearch, Name: "web"}); err != nil {
		t.Fatalf("InvokeCapability() error = %v", err)
	}
	if len(trace) != 1 || trace[0] != "search:web" {
		t.Fatalf("expected capability policy trace, got %#v", trace)
	}
}

func TestExtensionExecutionOrder(t *testing.T) {
	runner := New(Config{})
	providerDriver := &orderedExtensionProvider{}
	runner.RegisterProvider("ordered-extension", providerDriver)
	trace := make([]string, 0, 16)

	driver, err := toolkit.Tool("trace-tool", func(_ context.Context, input struct {
		Topic string `json:"topic"`
	}) (string, error) {
		trace = append(trace, "tool.handler")
		return "topic:" + input.Topic, nil
	})
	if err != nil {
		t.Fatalf("Tool() error = %v", err)
	}
	runner.RegisterTool(driver)

	runner.UseStageMiddleware(middleware.Func(func(ctx context.Context, envelope *middleware.Envelope, next middleware.Next) error {
		trace = append(trace, "stage."+string(envelope.Stage)+"."+envelope.Operation)
		return next(ctx, envelope)
	}))
	runner.RegisterHook(traceHook{trace: &trace})
	runner.UseCapabilityPolicy(capability.Func(func(ctx context.Context, call capability.Call, next capability.Next) (capability.Result, error) {
		trace = append(trace, "capability."+string(call.Type)+".policy")
		return next(ctx, call)
	}))

	sess, err := runner.CreateSession(context.Background(), session.CreateParams{Branch: "main"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, err := runner.Prompt(context.Background(), PromptRequest{
		SessionID: sess.ID,
		Provider:  "ordered-extension",
		Model:     "test",
		Messages:  []message.Message{message.NewText(message.RoleUser, "go")},
		Agent: AgentOptions{
			OutputGuardrails: []agent.OutputGuardrail{
				agent.NewOutputGuardrail("trace", func(_ context.Context, input agent.OutputGuardrailInput) (agent.OutputGuardrailResult, error) {
					trace = append(trace, "output_guardrail.check")
					return agent.AllowOutput(), nil
				}),
			},
		},
	}); err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}

	expected := []string{
		"stage.agent.prompt",
		"stage.llm.transform_context",
		"hook.transform_context",
		"stage.llm.before",
		"hook.before_model_call",
		"capability.llm.policy",
		"stage.llm.event",
		"hook.event",
		"stage.tool.before",
		"hook.before_tool_call",
		"capability.tool.policy",
		"tool.handler",
		"stage.tool.after",
		"hook.after_tool_call",
		"output_guardrail.check",
	}

	position := map[string]int{}
	for idx, item := range trace {
		if _, seen := position[item]; !seen {
			position[item] = idx
		}
	}
	for _, item := range expected {
		if _, ok := position[item]; !ok {
			t.Fatalf("expected trace %q in %#v", item, trace)
		}
	}
	for idx := 1; idx < len(expected); idx++ {
		if position[expected[idx-1]] >= position[expected[idx]] {
			t.Fatalf("expected %q before %q, got %#v", expected[idx-1], expected[idx], trace)
		}
	}
}

func TestStageMiddlewareBlockRecordsPolicyOutcome(t *testing.T) {
	runner := New(Config{})
	runner.RegisterProvider("ordered-extension", &orderedExtensionProvider{})
	blockErr := errors.New("blocked by stage middleware")
	runner.UseStageMiddleware(middleware.Func(func(ctx context.Context, envelope *middleware.Envelope, next middleware.Next) error {
		if envelope.Stage == middleware.StageAgent {
			return blockErr
		}
		return next(ctx, envelope)
	}))

	sess, err := runner.CreateSession(context.Background(), session.CreateParams{Branch: "main"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, err := runner.Prompt(context.Background(), PromptRequest{
		SessionID: sess.ID,
		Provider:  "ordered-extension",
		Model:     "test",
		Messages:  []message.Message{message.NewText(message.RoleUser, "go")},
	}); !errors.Is(err, blockErr) {
		t.Fatalf("expected block error, got %v", err)
	}

	events, err := runner.storage.Events().List(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected policy outcome event")
	}
	event := events[len(events)-1]
	if event.Type != storage.EventPolicyOutcome {
		t.Fatalf("expected policy outcome event, got %#v", event)
	}
	if got := event.Payload["policy"]; got != "stage.middleware" {
		t.Fatalf("expected stage middleware policy, got %#v", event.Payload)
	}
	if got := event.Payload["layer"]; got != "stage" {
		t.Fatalf("expected stage layer, got %#v", event.Payload)
	}
	if got := event.Payload["action"]; got != "block" {
		t.Fatalf("expected block action, got %#v", event.Payload)
	}
	if got := event.Payload["stage"]; got != "agent" {
		t.Fatalf("expected agent stage, got %#v", event.Payload)
	}
}
