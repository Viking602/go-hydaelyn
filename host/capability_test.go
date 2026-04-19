package host

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Viking602/go-hydaelyn/capability"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/session"
	"github.com/Viking602/go-hydaelyn/toolkit"
)

type capabilityProvider struct{}

func (capabilityProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "cap-provider"}
}

func (capabilityProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	if len(request.Messages) > 0 && request.Messages[len(request.Messages)-1].Role == message.RoleTool {
		return provider.NewSliceStream([]provider.Event{
			{Kind: provider.EventTextDelta, Text: "done"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}), nil
	}
	return provider.NewSliceStream([]provider.Event{
		{
			Kind: provider.EventToolCall,
			ToolCall: &message.ToolCall{
				ID:        "call-1",
				Name:      "lookup",
				Arguments: json.RawMessage(`{"query":"hydaelyn"}`),
			},
		},
		{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
	}), nil
}

func TestCapabilityMiddlewareObservesLLMAndToolCalls(t *testing.T) {
	runner := New(Config{})
	trace := make([]string, 0, 4)
	runner.UseCapabilityMiddleware(capability.Func(func(ctx context.Context, call capability.Call, next capability.Next) (capability.Result, error) {
		trace = append(trace, string(call.Type)+":"+call.Name)
		return next(ctx, call)
	}))
	runner.RegisterProvider("cap-provider", capabilityProvider{})
	driver, err := toolkit.Tool("lookup", func(_ context.Context, input struct {
		Query string `json:"query"`
	}) (string, error) {
		return "result:" + input.Query, nil
	})
	if err != nil {
		t.Fatalf("Tool() error = %v", err)
	}
	runner.RegisterTool(driver)
	sess, err := runner.CreateSession(context.Background(), session.CreateParams{Branch: "main"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	_, err = runner.Prompt(context.Background(), PromptRequest{
		SessionID: sess.ID,
		Provider:  "cap-provider",
		Model:     "test",
		Messages:  []message.Message{message.NewText(message.RoleUser, "go")},
	})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	if len(trace) < 2 {
		t.Fatalf("expected capability middleware trace, got %#v", trace)
	}
	if trace[0] != "llm:cap-provider" {
		t.Fatalf("expected llm capability first, got %#v", trace)
	}
	foundTool := false
	for _, item := range trace {
		if item == "tool:lookup" {
			foundTool = true
		}
	}
	if !foundTool {
		t.Fatalf("expected tool capability call, got %#v", trace)
	}
}
