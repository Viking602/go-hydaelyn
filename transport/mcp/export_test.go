package mcp

import (
	"encoding/json"
	"testing"

	"github.com/Viking602/venat/api"
)

func TestToolsFromCapabilities_ProjectsManifestCapabilities(t *testing.T) {
	schema := map[string]any{"type": "object"}
	manifest := api.CapabilityManifest{
		Name:    "research.core",
		Version: "0.8.0",
		Capabilities: []api.Capability{
			{
				Name:        "web_search",
				Description: "Run a keyword query",
				InputSchema: schema,
			},
			{
				Name:        "fetch_document",
				Description: "Fetch a document",
			},
		},
	}

	tools := ToolsFromCapabilities(manifest)
	if len(tools) != 2 {
		t.Fatalf("ToolsFromCapabilities() len = %d, want 2", len(tools))
	}
	if tools[0].Name != "web_search" || tools[0].Description != "Run a keyword query" {
		t.Fatalf("first tool = %#v", tools[0])
	}
	if tools[1].Name != "fetch_document" || tools[1].InputSchema["type"] != "object" {
		t.Fatalf("second tool = %#v", tools[1])
	}

	schema["type"] = "mutated"
	if tools[0].InputSchema["type"] != "object" {
		t.Fatalf("InputSchema was not copied: %#v", tools[0].InputSchema)
	}
}

func TestToolsFromCapabilities_DeepCopiesNestedSchema(t *testing.T) {
	properties := map[string]any{"q": map[string]any{"type": "string"}}
	schema := map[string]any{"type": "object", "properties": properties}
	tools := ToolsFromCapabilities(api.CapabilityManifest{
		Capabilities: []api.Capability{{Name: "web_search", InputSchema: schema}},
	})
	properties["q"] = map[string]any{"type": "mutated"}
	got, ok := tools[0].InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %#v", tools[0].InputSchema["properties"])
	}
	field, ok := got["q"].(map[string]any)
	if !ok || field["type"] != "string" {
		t.Fatalf("nested schema was not copied: %#v", got)
	}
}

func TestToolsFromCapabilities_EmptyManifest(t *testing.T) {
	if got := ToolsFromCapabilities(api.CapabilityManifest{}); len(got) != 0 {
		t.Fatalf("empty manifest tools = %#v", got)
	}
}

func TestToolDescriptor_JSONUsesMCPWireNames(t *testing.T) {
	raw, err := json.Marshal(ToolDescriptor{
		Name:        "web_search",
		Description: "Run a keyword query",
		InputSchema: map[string]any{"type": "object"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"name", "description", "inputSchema"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("missing MCP wire name %q in %s", key, raw)
		}
	}
}

func TestToolDescriptor_JSONEmitsObjectInputSchemaWhenMissing(t *testing.T) {
	raw, err := json.Marshal(ToolsFromCapabilities(api.CapabilityManifest{
		Capabilities: []api.Capability{{Name: "fetch_document"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"name":"fetch_document","inputSchema":{"type":"object"}}]`
	if string(raw) != want {
		t.Fatalf("json.Marshal(tools) = %s, want %s", raw, want)
	}
}
