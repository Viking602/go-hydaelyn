package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestServer(t *testing.T, handler func(method string, params map[string]any) any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		method, _ := payload["method"].(string)
		params, _ := payload["params"].(map[string]any)
		result := handler(method, params)
		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      payload["id"],
			"result":  result,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
}

func TestListResourcesReturnsServerResources(t *testing.T) {
	server := newTestServer(t, func(method string, _ map[string]any) any {
		if method != "resources/list" {
			t.Fatalf("unexpected method %s", method)
		}
		return map[string]any{
			"resources": []map[string]any{
				{"uri": "file:///docs/readme.md", "name": "README", "mimeType": "text/markdown"},
			},
		}
	})
	defer server.Close()

	client := New(NewHTTPTransport(server.URL, nil))
	resources, err := client.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources() error = %v", err)
	}
	if len(resources) != 1 || resources[0].URI != "file:///docs/readme.md" {
		t.Fatalf("unexpected resources %#v", resources)
	}
	if resources[0].Name != "README" {
		t.Fatalf("unexpected name %q", resources[0].Name)
	}
}

func TestReadResourceReturnsContents(t *testing.T) {
	server := newTestServer(t, func(method string, params map[string]any) any {
		if method != "resources/read" {
			t.Fatalf("unexpected method %s", method)
		}
		if params["uri"] != "file:///docs/readme.md" {
			t.Fatalf("unexpected uri %v", params["uri"])
		}
		return map[string]any{
			"contents": []map[string]any{
				{"uri": "file:///docs/readme.md", "text": "# Hello", "mimeType": "text/markdown"},
			},
		}
	})
	defer server.Close()

	client := New(NewHTTPTransport(server.URL, nil))
	contents, err := client.ReadResource(context.Background(), "file:///docs/readme.md")
	if err != nil {
		t.Fatalf("ReadResource() error = %v", err)
	}
	if len(contents) != 1 || contents[0].Text != "# Hello" {
		t.Fatalf("unexpected contents %#v", contents)
	}
}

func TestListPromptsReturnsServerPrompts(t *testing.T) {
	server := newTestServer(t, func(method string, _ map[string]any) any {
		if method != "prompts/list" {
			t.Fatalf("unexpected method %s", method)
		}
		return map[string]any{
			"prompts": []map[string]any{
				{
					"name":        "summarize",
					"description": "Summarize text",
					"arguments": []map[string]any{
						{"name": "text", "required": true},
					},
				},
			},
		}
	})
	defer server.Close()

	client := New(NewHTTPTransport(server.URL, nil))
	prompts, err := client.ListPrompts(context.Background())
	if err != nil {
		t.Fatalf("ListPrompts() error = %v", err)
	}
	if len(prompts) != 1 || prompts[0].Name != "summarize" {
		t.Fatalf("unexpected prompts %#v", prompts)
	}
	if len(prompts[0].Arguments) != 1 || !prompts[0].Arguments[0].Required {
		t.Fatalf("unexpected arguments %#v", prompts[0].Arguments)
	}
}

func TestGetPromptReturnsMessages(t *testing.T) {
	server := newTestServer(t, func(method string, params map[string]any) any {
		if method != "prompts/get" {
			t.Fatalf("unexpected method %s", method)
		}
		if params["name"] != "summarize" {
			t.Fatalf("unexpected name %v", params["name"])
		}
		return map[string]any{
			"messages": []map[string]any{
				{
					"role":    "user",
					"content": map[string]any{"type": "text", "text": "Summarize: hello world"},
				},
			},
		}
	})
	defer server.Close()

	client := New(NewHTTPTransport(server.URL, nil))
	messages, err := client.GetPrompt(context.Background(), "summarize", map[string]string{"text": "hello world"})
	if err != nil {
		t.Fatalf("GetPrompt() error = %v", err)
	}
	if len(messages) != 1 || messages[0].Role != "user" {
		t.Fatalf("unexpected messages %#v", messages)
	}
	if messages[0].Content.Text != "Summarize: hello world" {
		t.Fatalf("unexpected content %#v", messages[0].Content)
	}
}
