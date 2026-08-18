package mcp

import "github.com/Viking602/venat/api"

// ToolDescriptor is the MCP-facing projection of a capability.
type ToolDescriptor struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// godoc-allow-any: JSON Schema is represented as an open object.
	InputSchema map[string]any `json:"inputSchema"`
}

// ToolsFromCapabilities projects a capability manifest into MCP tool
// descriptors. OpenAPI and CLI renderers remain deferred.
func ToolsFromCapabilities(manifest api.CapabilityManifest) []ToolDescriptor {
	out := make([]ToolDescriptor, 0, len(manifest.Capabilities))
	for _, capability := range manifest.Capabilities {
		out = append(out, ToolDescriptor{
			Name:        capability.Name,
			Description: capability.Description,
			InputSchema: mcpInputSchema(capability.InputSchema),
		})
	}
	return out
}

func mcpInputSchema(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{"type": "object"}
	}
	return cloneAnyMap(in)
}

func cloneAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
