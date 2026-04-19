package host

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Viking602/go-hydaelyn/mcp"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/plugin"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/session"
	"github.com/Viking602/go-hydaelyn/tool"
	"github.com/Viking602/go-hydaelyn/toolkit"
)

type fakeGateway struct {
	drivers []tool.Driver
}

func (g fakeGateway) ImportTools(context.Context) ([]tool.Driver, error) {
	return g.drivers, nil
}

type gatewayProvider struct{}

func (gatewayProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "gateway-provider"}
}

func (gatewayProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
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
				Name:      "mcp_lookup",
				Arguments: json.RawMessage(`{"query":"hydaelyn"}`),
			},
		},
		{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
	}), nil
}

var _ mcp.Gateway = fakeGateway{}

func TestMCPGatewayPluginImportsTools(t *testing.T) {
	runner := New(Config{})
	runner.RegisterProvider("gateway-provider", gatewayProvider{})
	driver, err := toolkit.Tool("mcp_lookup", func(_ context.Context, input struct {
		Query string `json:"query"`
	}) (string, error) {
		return "result:" + input.Query, nil
	})
	if err != nil {
		t.Fatalf("Tool() error = %v", err)
	}
	if err := runner.RegisterPlugin(plugin.Spec{
		Type:      plugin.TypeMCPGateway,
		Name:      "fake-gateway",
		Component: fakeGateway{drivers: []tool.Driver{driver}},
	}); err != nil {
		t.Fatalf("RegisterPlugin() error = %v", err)
	}
	sess, err := runner.CreateSession(context.Background(), session.CreateParams{Branch: "main"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	response, err := runner.Prompt(context.Background(), PromptRequest{
		SessionID: sess.ID,
		Provider:  "gateway-provider",
		Model:     "test",
		Messages:  []message.Message{message.NewText(message.RoleUser, "hello")},
	})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	if len(response.Messages) == 0 {
		t.Fatalf("expected prompt output, got %#v", response)
	}
}
