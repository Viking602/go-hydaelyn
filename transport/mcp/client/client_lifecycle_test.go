package mcpclient

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestClientOperationsReturnNotInitializedBeforeInitialize(t *testing.T) {
	tests := []struct {
		name string
		call func(*Client) error
	}{
		{"list tools", func(client *Client) error { _, err := client.ListTools(context.Background()); return err }},
		{"call tool", func(client *Client) error { _, err := client.CallTool(context.Background(), "echo", nil); return err }},
		{"list resources", func(client *Client) error { _, err := client.ListResources(context.Background()); return err }},
		{"read resource", func(client *Client) error {
			_, err := client.ReadResource(context.Background(), "file:///x")
			return err
		}},
		{"list prompts", func(client *Client) error { _, err := client.ListPrompts(context.Background()); return err }},
		{"get prompt", func(client *Client) error { _, err := client.GetPrompt(context.Background(), "p", nil); return err }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			_, clientTransport := sdkmcp.NewInMemoryTransports()
			client := New(clientTransport)

			// When
			err := test.call(client)

			// Then
			if !errors.Is(err, ErrNotInitialized) {
				t.Fatalf("operation error = %v, want ErrNotInitialized", err)
			}
		})
	}
}

func TestClientCloseIsIdempotent(t *testing.T) {
	// Given
	client := newInitializedTestClient(t)

	// When
	firstErr := client.Close()
	secondErr := client.Close()

	// Then
	if !errors.Is(firstErr, secondErr) && !errors.Is(secondErr, firstErr) {
		t.Fatalf("Close() errors differ: first=%v second=%v", firstErr, secondErr)
	}
}

func TestClientCloseCancelsBlockingCall(t *testing.T) {
	// Given
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "server", Version: "v1.0.0"}, nil)
	started := make(chan struct{})
	releaseServer := make(chan struct{})
	release := func() {
		select {
		case <-releaseServer:
		default:
			close(releaseServer)
		}
	}
	t.Cleanup(release)
	server.AddTool(&sdkmcp.Tool{Name: "wait", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		close(started)
		<-releaseServer
		return &sdkmcp.CallToolResult{}, nil
	})
	serverCtx, cancelServer := context.WithCancel(context.Background())
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Run(serverCtx, serverTransport) }()
	client := New(clientTransport)
	if _, err := client.Initialize(context.Background(), "client", "v1.0.0"); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	callDone := make(chan error, 1)
	go func() {
		_, err := client.CallTool(context.Background(), "wait", nil)
		callDone <- err
	}()
	<-started

	// When
	closeDone := make(chan error, 1)
	go func() { closeDone <- client.Close() }()

	// Then
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close() blocked behind an in-flight call")
	}
	select {
	case err := <-callDone:
		if err == nil {
			t.Fatal("CallTool() returned nil after Close")
		}
	case <-time.After(time.Second):
		t.Fatal("Close() did not cancel the in-flight call")
	}
	release()
	cancelServer()
	select {
	case err := <-serverDone:
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
			t.Fatalf("server.Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server.Run() did not stop after cleanup")
	}
}

func TestClientInitializeReturnsInvalidTransportForUntypedNil(t *testing.T) {
	// Given
	client := New(nil)

	// When
	_, err := client.Initialize(context.Background(), "test-client", "v1.0.0")

	// Then
	if !errors.Is(err, ErrInvalidTransport) {
		t.Fatalf("Initialize() error = %v, want ErrInvalidTransport", err)
	}
}

func TestClientInitializeReturnsInvalidTransportForTypedNil(t *testing.T) {
	// Given
	var transport *nilDereferenceTransport
	client := New(transport)

	// When
	_, err := client.Initialize(context.Background(), "test-client", "v1.0.0")

	// Then
	if !errors.Is(err, ErrInvalidTransport) {
		t.Fatalf("Initialize() error = %v, want ErrInvalidTransport", err)
	}
}

func TestClientCloseReturnsInvalidTransportForTypedNil(t *testing.T) {
	// Given
	var transport *nilDereferenceTransport
	client := New(transport)

	// When
	err := client.Close()

	// Then
	if !errors.Is(err, ErrInvalidTransport) {
		t.Fatalf("Close() error = %v, want ErrInvalidTransport", err)
	}
}

type nilDereferenceTransport struct {
	connected bool
}

func (t *nilDereferenceTransport) Connect(context.Context) (sdkmcp.Connection, error) {
	t.connected = true
	return nil, nil
}

func (t *nilDereferenceTransport) Close() error {
	t.connected = true
	return nil
}
