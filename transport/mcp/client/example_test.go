package mcpclient_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	mcpclient "github.com/Viking602/go-hydaelyn/transport/mcp/client"
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
	// Spin up a minimal MCP server that responds to tools/list.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"result": map[string]any{
				"tools": []map[string]any{
					{"name": "search", "description": "Search the web"},
				},
			},
		})
	}))
	defer server.Close()

	c := mcpclient.New(mcpclient.NewHTTPTransport(server.URL, nil))
	defer func() { _ = c.Close() }()

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
