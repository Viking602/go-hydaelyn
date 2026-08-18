package mcp

import (
	"encoding/json"
	"reflect"

	"github.com/Viking602/venat/api"
)

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
		out[key] = cloneJSONValue(value)
	}
	return out
}

func cloneJSONValue(value any) any {
	if value == nil {
		return nil
	}
	if raw, ok := value.(json.RawMessage); ok {
		return append(json.RawMessage(nil), raw...)
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Map:
		if rv.IsNil() {
			return value
		}
		out := reflect.MakeMapWithSize(rv.Type(), rv.Len())
		for iter := rv.MapRange(); iter.Next(); {
			cloned := cloneJSONValue(iter.Value().Interface())
			cv := reflect.ValueOf(cloned)
			if !cv.IsValid() {
				cv = reflect.Zero(iter.Value().Type())
			}
			out.SetMapIndex(iter.Key(), cv)
		}
		return out.Interface()
	case reflect.Slice:
		if rv.IsNil() {
			return value
		}
		out := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
		for i := 0; i < rv.Len(); i++ {
			cloned := cloneJSONValue(rv.Index(i).Interface())
			if cloned == nil {
				continue
			}
			elem := out.Index(i)
			cv := reflect.ValueOf(cloned)
			if cv.Type().AssignableTo(elem.Type()) {
				elem.Set(cv)
			}
		}
		return out.Interface()
	default:
		return value
	}
}
