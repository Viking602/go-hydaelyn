package toolkit

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Viking602/go-hydaelyn/tool"
	mcpclient "github.com/Viking602/go-hydaelyn/transport/mcp/client"
)

func TestImportMCPToolsMapsExternalToolsToLocalDrivers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		method, _ := payload["method"].(string)
		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      payload["id"],
		}
		switch method {
		case "tools/list":
			response["result"] = map[string]any{
				"tools": []map[string]any{
					{
						"name":        "external_search",
						"description": "search through mcp",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"query": map[string]any{"type": "string"},
							},
						},
					},
				},
			}
		case "tools/call":
			response["result"] = map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "mcp-result"},
				},
				"structuredContent": map[string]any{
					"query": "golang agents",
				},
			}
		default:
			response["result"] = map[string]any{}
		}
		body, _ := json.Marshal(response)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(body)
	}))
	defer server.Close()

	client := mcpclient.New(mcpclient.NewHTTPTransport(server.URL, nil))
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
	if result.Content != "mcp-result" {
		t.Fatalf("unexpected tool result: %#v", result)
	}
}
