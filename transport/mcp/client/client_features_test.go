package mcpclient

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/Viking602/venat/transport/mcpcontract"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestClientListToolsMapsOfficialTool(t *testing.T) {
	// Given
	client := newInitializedTestClient(t)

	// When
	tools, err := client.ListTools(context.Background())
	// Then
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("ListTools() count = %d, want 2", len(tools))
	}
	if tools[0].Name != "echo" || tools[0].InputSchema.Type != "object" {
		t.Fatalf("ListTools() first tool = %#v", tools[0])
	}
}

func TestClientCallToolMapsOfficialResult(t *testing.T) {
	// Given
	client := newInitializedTestClient(t)

	// When
	result, err := client.CallTool(context.Background(), "echo", map[string]any{"text": "hello"})
	// Then
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "hello" {
		t.Fatalf("CallTool() content = %#v", result.Content)
	}
	if result.StructuredContent["text"] != "hello" {
		t.Fatalf("CallTool() structured content = %#v", result.StructuredContent)
	}
}

func TestClientCallToolPreservesTypedRPCError(t *testing.T) {
	// Given
	client := newInitializedTestClient(t)

	// When
	_, err := client.CallTool(context.Background(), "fail", nil)

	// Then
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("CallTool() error = %T %v, want *RPCError", err, err)
	}
	if rpcErr.Code != -32001 || string(rpcErr.Data) != `{"detail":"boom"}` {
		t.Fatalf("CallTool() RPC error = %#v", rpcErr)
	}
	var sdkErr *jsonrpc.Error
	if !errors.As(err, &sdkErr) {
		t.Fatalf("CallTool() error does not preserve SDK error: %v", err)
	}
}

func TestClientResourcesMapOfficialValues(t *testing.T) {
	// Given
	client := newInitializedTestClient(t)

	// When
	resources, err := client.ListResources(context.Background())
	// Then
	if err != nil {
		t.Fatalf("ListResources() error = %v", err)
	}
	if len(resources) != 1 || resources[0].URI != "file:///readme.md" {
		t.Fatalf("ListResources() = %#v", resources)
	}
}

func TestClientResourceTemplatesMapOfficialValues(t *testing.T) {
	client := newInitializedTestClient(t)
	templates, err := client.ListResourceTemplates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(templates) != 1 || templates[0].URITemplate != "file:///{name}.md" {
		t.Fatalf("resource templates = %#v", templates)
	}
}

func TestClientReadResourceMapsOfficialContent(t *testing.T) {
	// Given
	client := newInitializedTestClient(t)

	// When
	contents, err := client.ReadResource(context.Background(), "file:///readme.md")
	// Then
	if err != nil {
		t.Fatalf("ReadResource() error = %v", err)
	}
	if len(contents) != 1 || contents[0].Text != "# Venat" {
		t.Fatalf("ReadResource() = %#v", contents)
	}
}

func TestClientPromptsMapOfficialValues(t *testing.T) {
	// Given
	client := newInitializedTestClient(t)

	// When
	prompts, err := client.ListPrompts(context.Background())
	// Then
	if err != nil {
		t.Fatalf("ListPrompts() error = %v", err)
	}
	if len(prompts) != 1 || prompts[0].Name != "summarize" || !prompts[0].Arguments[0].Required {
		t.Fatalf("ListPrompts() = %#v", prompts)
	}
}

func TestClientGetPromptMapsOfficialMessage(t *testing.T) {
	// Given
	client := newInitializedTestClient(t)

	// When
	messages, err := client.GetPrompt(context.Background(), "summarize", map[string]string{"text": "hello"})
	// Then
	if err != nil {
		t.Fatalf("GetPrompt() error = %v", err)
	}
	if len(messages) != 1 || messages[0].Role != "user" || messages[0].Content.Text != "Summarize: hello" {
		t.Fatalf("GetPrompt() = %#v", messages)
	}
}

func TestClientForwardsNotificationsAndResourceSubscriptions(t *testing.T) {
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	server := newFeatureTestServerWithOptions(&sdkmcp.ServerOptions{
		SubscribeHandler:   func(context.Context, *sdkmcp.SubscribeRequest) error { return nil },
		UnsubscribeHandler: func(context.Context, *sdkmcp.UnsubscribeRequest) error { return nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Run(ctx, serverTransport) }()
	notifications := make(chan mcpcontract.Notification, 16)
	client := NewWithOptions(clientTransport, Options{NotificationHandler: func(_ context.Context, notification mcpcontract.Notification) {
		notifications <- notification
	}})
	if _, err := client.Initialize(context.Background(), "notification-client", "v1"); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		cancel()
		<-serverDone
	})
	server.AddPrompt(&sdkmcp.Prompt{Name: "late"}, func(context.Context, *sdkmcp.GetPromptRequest) (*sdkmcp.GetPromptResult, error) {
		return &sdkmcp.GetPromptResult{}, nil
	})
	waitNotificationKind(t, notifications, "prompts/list_changed")
	if err := client.SubscribeResource(context.Background(), "file:///readme.md"); err != nil {
		t.Fatal(err)
	}
	if err := server.ResourceUpdated(context.Background(), &sdkmcp.ResourceUpdatedNotificationParams{URI: "file:///readme.md"}); err != nil {
		t.Fatal(err)
	}
	updated := waitNotificationKind(t, notifications, "resources/updated")
	if updated.URI != "file:///readme.md" {
		t.Fatalf("resource update = %#v", updated)
	}
	if err := client.UnsubscribeResource(context.Background(), "file:///readme.md"); err != nil {
		t.Fatal(err)
	}
}

func waitNotificationKind(t *testing.T, notifications <-chan mcpcontract.Notification, kind string) mcpcontract.Notification {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case notification := <-notifications:
			if notification.Kind == kind {
				return notification
			}
		case <-timer.C:
			t.Fatalf("notification %q was not received", kind)
		}
	}
}

func newInitializedTestClient(t *testing.T) *Client {
	t.Helper()
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	server := newFeatureTestServer()
	ctx, cancel := context.WithCancel(context.Background())
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run(ctx, serverTransport)
	}()
	client := New(clientTransport)
	if _, err := client.Initialize(context.Background(), "test-client", "v1.0.0"); err != nil {
		cancel()
		t.Fatalf("Initialize() error = %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		cancel()
		err := <-serverDone
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
			t.Errorf("server.Run() error = %v", err)
		}
	})
	return client
}

func newFeatureTestServer() *sdkmcp.Server {
	return newFeatureTestServerWithOptions(nil)
}

func newFeatureTestServerWithOptions(options *sdkmcp.ServerOptions) *sdkmcp.Server {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test-server", Version: "v1.0.0"}, options)
	type echoArguments struct {
		Text string `json:"text"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "echo",
		Description: "Echo text",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string"},
			},
		},
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, arguments echoArguments) (*sdkmcp.CallToolResult, map[string]any, error) {
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: arguments.Text}},
		}, map[string]any{"text": arguments.Text}, nil
	})
	server.AddTool(&sdkmcp.Tool{Name: "fail", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return nil, &jsonrpc.Error{Code: -32001, Message: "tool failed", Data: []byte(`{"detail":"boom"}`)}
	})
	server.AddResource(&sdkmcp.Resource{URI: "file:///readme.md", Name: "README", MIMEType: "text/markdown"}, func(context.Context, *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
		return &sdkmcp.ReadResourceResult{Contents: []*sdkmcp.ResourceContents{{URI: "file:///readme.md", MIMEType: "text/markdown", Text: "# Venat"}}}, nil
	})
	server.AddResourceTemplate(&sdkmcp.ResourceTemplate{URITemplate: "file:///{name}.md", Name: "Markdown"}, func(context.Context, *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
		return &sdkmcp.ReadResourceResult{}, nil
	})
	server.AddPrompt(&sdkmcp.Prompt{
		Name: "summarize",
		Arguments: []*sdkmcp.PromptArgument{
			{Name: "text", Required: true},
		},
	}, func(_ context.Context, request *sdkmcp.GetPromptRequest) (*sdkmcp.GetPromptResult, error) {
		return &sdkmcp.GetPromptResult{Messages: []*sdkmcp.PromptMessage{{
			Role:    sdkmcp.Role("user"),
			Content: &sdkmcp.TextContent{Text: "Summarize: " + request.Params.Arguments["text"]},
		}}}, nil
	})
	return server
}
