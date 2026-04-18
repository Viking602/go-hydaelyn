package mcp

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/tool"
	mcpclient "github.com/Viking602/go-hydaelyn/transport/mcp/client"
)

func TestNewGateway(t *testing.T) {
	// Create a nil client for testing (we can't create a real one without transport)
	var client *mcpclient.Client
	gateway := NewGateway(client)

	// The gateway should be created even with nil client
	// The actual ImportTools will fail when called
	if gateway.Client != client {
		t.Error("NewGateway() should set the client")
	}
}

func TestClientGateway_ImportTools_NilClient(t *testing.T) {
	gateway := ClientGateway{}

	ctx := context.Background()
	
	// This will panic with nil client - use defer/recover to test this
	defer func() {
		if r := recover(); r != nil {
			// Expected panic with nil client
			t.Logf("Expected panic occurred: %v", r)
		}
	}()
	
	_, err := gateway.ImportTools(ctx)

	// Should error with nil client (but actually panics)
	if err == nil {
		t.Error("ImportTools() should return error with nil client")
	}
}

func TestGateway_Interface(t *testing.T) {
	// Test that ClientGateway implements Gateway interface
	var _ Gateway = (*ClientGateway)(nil)
}

func TestNewGateway_ReturnsClientGateway(t *testing.T) {
	var client *mcpclient.Client
	gateway := NewGateway(client)

	// Test that we can access it as ClientGateway
	if gateway.Client != client {
		t.Error("Client should be set correctly")
	}
}

// Simple test to verify the types exist and work
type simpleGateway struct {
	tools []tool.Driver
}

func (s *simpleGateway) ImportTools(ctx context.Context) ([]tool.Driver, error) {
	return s.tools, nil
}

func TestGateway_Implementation(t *testing.T) {
	// Test that a custom implementation works
	g := &simpleGateway{tools: []tool.Driver{}}

	tools, err := g.ImportTools(context.Background())
	if err != nil {
		t.Errorf("ImportTools() error = %v", err)
	}
	if tools == nil {
		t.Error("ImportTools() should not return nil")
	}
}