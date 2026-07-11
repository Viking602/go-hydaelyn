package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/go-hydaelyn/tool"
	"github.com/Viking602/go-hydaelyn/tool/kit"
	mcpclient "github.com/Viking602/go-hydaelyn/transport/mcp/client"
)

func TestNewGateway(t *testing.T) {
	// Create a nil client for testing (we can't create a real one without transport)
	gateway := NewGateway(nil)

	// The gateway should be created even with nil client
	// The actual ImportTools will fail when called
	if gateway.Client != nil {
		t.Error("NewGateway() should set the client")
	}
}

func TestErrInvalidClientAliasesKitSentinel(t *testing.T) {
	if ErrInvalidClient != kit.ErrInvalidMCPClient {
		t.Fatal("ErrInvalidClient should alias kit.ErrInvalidMCPClient")
	}
}

func TestClientGatewayImportToolsReturnsInvalidClientForUntypedNil(t *testing.T) {
	// Given
	gateway := NewGateway(nil)

	// When
	_, err := gateway.ImportTools(context.Background())

	// Then
	if !errors.Is(err, ErrInvalidClient) {
		t.Fatalf("ImportTools() error = %v, want ErrInvalidClient", err)
	}
}

func TestClientGatewayImportToolsReturnsInvalidClientForTypedNil(t *testing.T) {
	// Given
	var client *mcpclient.Client
	gateway := NewGateway(client)
	if gateway.Client != nil {
		t.Fatal("NewGateway() should normalize a typed-nil client")
	}

	// When
	_, err := gateway.ImportTools(context.Background())

	// Then
	if !errors.Is(err, ErrInvalidClient) {
		t.Fatalf("ImportTools() error = %v, want ErrInvalidClient", err)
	}
}

func TestGatewayInterface(t *testing.T) {
	// Test that ClientGateway implements Gateway interface
	var _ Gateway = (*ClientGateway)(nil)
}

func TestNewGatewayReturnsClientGateway(t *testing.T) {
	gateway := NewGateway(nil)

	// Test that we can access it as ClientGateway
	if gateway.Client != nil {
		t.Error("Client should be set correctly")
	}
}

// Simple test to verify the types exist and work
type simpleGateway struct {
	tools []tool.Driver
}

func (s *simpleGateway) ImportTools(_ context.Context) ([]tool.Driver, error) {
	return s.tools, nil
}

func TestGatewayImplementation(t *testing.T) {
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
