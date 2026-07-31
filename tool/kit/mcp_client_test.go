package kit

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Viking602/venat/tool"
	mcpclient "github.com/Viking602/venat/transport/mcp/client"
)

func TestImportMCPToolsMapsExternalToolsToLocalDrivers(t *testing.T) {
	// Given
	type searchArguments struct {
		Query string `json:"query"`
	}
	mcpServer := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test-server", Version: "v1.0.0"}, nil)
	sdkmcp.AddTool(mcpServer, &sdkmcp.Tool{Name: "external_search", Description: "search through mcp"}, func(_ context.Context, _ *sdkmcp.CallToolRequest, arguments searchArguments) (*sdkmcp.CallToolResult, map[string]any, error) {
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "mcp-result"}},
		}, map[string]any{"query": arguments.Query}, nil
	})
	server := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server {
		return mcpServer
	}, nil))
	t.Cleanup(server.Close)

	client := mcpclient.New(mcpclient.NewHTTPTransport(server.URL, nil))
	if _, err := client.Initialize(context.Background(), "test-client", "v1.0.0"); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	// When
	drivers, err := ImportMCPTools(context.Background(), client)
	if err != nil {
		t.Fatalf("ImportMCPTools() error = %v", err)
	}
	if len(drivers) != 1 {
		t.Fatalf("expected 1 imported tool, got %d", len(drivers))
	}
	result, err := drivers[0].Execute(context.Background(), tool.Call{
		ID:        "call-1",
		Name:      "external_search",
		Arguments: bytes.TrimSpace([]byte(`{"query":"golang agents"}`)),
	}, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// Then
	if result.Content != "mcp-result" {
		t.Fatalf("unexpected tool result: %#v", result)
	}
}

func TestImportMCPToolsReturnsInvalidClientForUntypedNil(t *testing.T) {
	// When
	_, err := ImportMCPTools(context.Background(), nil)

	// Then
	if !errors.Is(err, ErrInvalidMCPClient) {
		t.Fatalf("ImportMCPTools() error = %v, want ErrInvalidMCPClient", err)
	}
}

func TestImportMCPToolsReturnsInvalidClientForTypedNil(t *testing.T) {
	// Given
	var client *mcpclient.Client

	// When
	_, err := ImportMCPTools(context.Background(), client)

	// Then
	if !errors.Is(err, ErrInvalidMCPClient) {
		t.Fatalf("ImportMCPTools() error = %v, want ErrInvalidMCPClient", err)
	}
}
