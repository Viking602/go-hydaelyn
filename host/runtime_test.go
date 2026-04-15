package host

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/session"
	"github.com/Viking602/go-hydaelyn/toolkit"
)

type fakeProvider struct{}

func (fakeProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "fake"}
}

func (fakeProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	if len(request.Messages) > 0 && request.Messages[len(request.Messages)-1].Role == message.RoleTool {
		return provider.NewSliceStream([]provider.Event{
			{Kind: provider.EventTextDelta, Text: "complete"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}), nil
	}
	return provider.NewSliceStream([]provider.Event{
		{
			Kind: provider.EventToolCall,
			ToolCall: &message.ToolCall{
				ID:        "call-1",
				Name:      "answer",
				Arguments: json.RawMessage(`{"topic":"mcp"}`),
			},
		},
		{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
	}), nil
}

func TestRuntimePrompt(t *testing.T) {
	runtime := New(Config{})
	runtime.RegisterProvider("fake", fakeProvider{})
	driver, err := toolkit.Tool("answer", func(_ context.Context, input struct {
		Topic string `json:"topic"`
	}) (string, error) {
		return "topic:" + input.Topic, nil
	})
	if err != nil {
		t.Fatalf("Tool() error = %v", err)
	}
	runtime.RegisterTool(driver)
	sess, err := runtime.CreateSession(context.Background(), session.CreateParams{Branch: "main"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	response, err := runtime.Prompt(context.Background(), PromptRequest{
		SessionID: sess.ID,
		Provider:  "fake",
		Model:     "test",
		Messages:  []message.Message{message.NewText(message.RoleUser, "go")},
	})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	if len(response.Messages) != 3 {
		t.Fatalf("expected assistant/tool/assistant chain, got %d messages", len(response.Messages))
	}
}
