package tooltest

import (
	"context"
	"encoding/json"
	"testing"

	"hydaelyn/tool"
)

func MustCall(t *testing.T, driver tool.Driver, payload any) tool.Result {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	result, err := driver.Execute(context.Background(), tool.Call{
		ID:        "call-1",
		Name:      driver.Definition().Name,
		Arguments: raw,
	}, nil)
	if err != nil {
		t.Fatalf("execute tool: %v", err)
	}
	return result
}

func MustSchema(t *testing.T, driver tool.Driver) tool.Schema {
	t.Helper()
	schema := driver.Definition().InputSchema
	if schema.Type == "" {
		t.Fatalf("tool schema is empty")
	}
	return schema
}
