package mcpclient_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	mcpclient "github.com/Viking602/venat/transport/mcp/client"
)

// Example demonstrates the standard usage of the mcpclient package: connect to
// an MCP server (HTTP transport here) and discover its tools.
//
// In production code, replace the httptest server with a real endpoint:
//
//	c := mcpclient.New(mcpclient.NewHTTPTransport("https://example.com/mcp", nil))
//
// or use [mcpclient.DialStdio] for stdio-based MCP servers.
func Example() {
	serverImplementation := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "example-server", Version: "v1.0.0"}, nil)
	serverImplementation.AddTool(&sdkmcp.Tool{
		Name:        "search",
		Description: "Search the web",
		InputSchema: map[string]any{"type": "object"},
	}, nil)
	server := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server {
		return serverImplementation
	}, nil))
	defer server.Close()

	c := mcpclient.New(mcpclient.NewHTTPTransport(server.URL, nil))
	defer func() { _ = c.Close() }()

	if _, err := c.Initialize(context.Background(), "example-client", "v1.0.0"); err != nil {
		fmt.Println("error:", err)
		return
	}
	tools, err := c.ListTools(context.Background())
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	for _, t := range tools {
		fmt.Printf("%s: %s\n", t.Name, t.Description)
	}
	// Output:
	// search: Search the web
}
