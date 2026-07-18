package mcpclient

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/transport/mcpcontract"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestClientInitializeConnectsOfficialSession(t *testing.T) {
	// Given
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test-server", Version: "v1.0.0"}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run(ctx, serverTransport)
	}()
	client := New(clientTransport)
	t.Cleanup(func() {
		_ = client.Close()
		cancel()
		<-serverDone
	})

	// When
	result, err := client.Initialize(context.Background(), "test-client", "v1.0.0")

	// Then
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if result.ProtocolVersion == "" {
		t.Fatal("Initialize() protocol version is empty")
	}
	if result.ServerInfo.Name != "test-server" {
		t.Fatalf("Initialize() server name = %q, want test-server", result.ServerInfo.Name)
	}
}

func TestClientElicitationHandlerBridgesOfficialSession(t *testing.T) {
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test-server", Version: "v1.0.0"}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server Connect() error = %v", err)
	}
	defer serverSession.Close()
	client := NewWithOptions(clientTransport, Options{ElicitationHandler: func(_ context.Context, request mcpcontract.Elicitation) (mcpcontract.ElicitationResult, error) {
		if request.Mode != "form" || request.Message != "Choose" {
			t.Fatalf("elicitation request = %#v", request)
		}
		return mcpcontract.ElicitationResult{Action: "accept", Content: map[string]any{"answer": "yes"}}, nil
	}})
	t.Cleanup(func() { _ = client.Close() })
	if _, err := client.Initialize(context.Background(), "test-client", "v1.0.0"); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	result, err := serverSession.Elicit(context.Background(), &sdkmcp.ElicitParams{
		Mode: "form", Message: "Choose", RequestedSchema: map[string]any{
			"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}},
		},
	})
	if err != nil {
		t.Fatalf("Elicit() error = %v", err)
	}
	if result.Action != "accept" || result.Content["answer"] != "yes" {
		t.Fatalf("Elicit() result = %#v", result)
	}
}
