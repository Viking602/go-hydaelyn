package mcpclient

import (
	"context"
	"testing"

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
