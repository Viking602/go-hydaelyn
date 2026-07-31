package tooltest

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Viking602/venat/tool"
)

type mockDriver struct {
	definition tool.Definition
	result     tool.Result
	err        error
}

func (m *mockDriver) Definition() tool.Definition {
	return m.definition
}

func (m *mockDriver) Execute(_ context.Context, _ tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	return m.result, m.err
}

func TestMustCallSuccess(t *testing.T) {
	driver := &mockDriver{
		definition: tool.Definition{
			Name:        "test-tool",
			Description: "A test tool",
			InputSchema: tool.Schema{Type: "object"},
		},
		result: tool.Result{
			Content: "success",
		},
	}

	payload := map[string]string{"key": "value"}
	result := MustCall(t, driver, payload)

	if result.Content != "success" {
		t.Errorf("Result.Content = %v, want success", result.Content)
	}
}

func TestMustSchemaSuccess(t *testing.T) {
	driver := &mockDriver{
		definition: tool.Definition{
			Name:        "test-tool",
			Description: "A test tool",
			InputSchema: tool.Schema{
				Type: "object",
				Properties: map[string]tool.Schema{
					"name": {Type: "string"},
				},
			},
		},
	}

	schema := MustSchema(t, driver)

	if schema.Type != "object" {
		t.Errorf("Schema.Type = %v, want object", schema.Type)
	}
}

func TestMustCallWithComplexPayload(t *testing.T) {
	driver := &mockDriver{
		definition: tool.Definition{
			Name: "complex-tool",
		},
		result: tool.Result{
			Content:    `{"data": "result"}`,
			Structured: json.RawMessage(`{"data": "result"}`),
		},
	}

	payload := map[string]any{
		"name": "test",
		"nested": map[string]any{
			"value": 123,
		},
		"items": []string{"a", "b", "c"},
	}

	result := MustCall(t, driver, payload)

	if result.Content == "" {
		t.Error("Result.Content should not be empty")
	}
}

func TestMustCallResultFields(t *testing.T) {
	driver := &mockDriver{
		definition: tool.Definition{Name: "test"},
		result: tool.Result{
			Content:    "output",
			Structured: json.RawMessage(`{"key":"value"}`),
			IsError:    false,
		},
	}

	payload := map[string]string{}
	result := MustCall(t, driver, payload)

	if result.IsError {
		t.Error("Result.IsError should be false")
	}
	if result.Structured == nil {
		t.Error("Result.Structured should not be nil")
	}
}
