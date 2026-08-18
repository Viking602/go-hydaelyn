package mcp

import (
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
	if tools[1].Name != "fetch_document" || tools[1].InputSchema != nil {
		t.Fatalf("second tool = %#v", tools[1])
	}

	schema["type"] = "mutated"
	if tools[0].InputSchema["type"] != "object" {
		t.Fatalf("InputSchema was not copied: %#v", tools[0].InputSchema)
	}
}

func TestToolsFromCapabilities_EmptyManifest(t *testing.T) {
	if got := ToolsFromCapabilities(api.CapabilityManifest{}); len(got) != 0 {
		t.Fatalf("empty manifest tools = %#v", got)
	}
}
