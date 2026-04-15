package toolkit

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Viking602/go-hydaelyn/tool"
	"github.com/Viking602/go-hydaelyn/tooltest"
)

func TestToolWrapsFunctionAndGeneratesSchema(t *testing.T) {
	type input struct {
		Query string `json:"query" description:"search query"`
		Limit int    `json:"limit,omitempty"`
	}
	driver, err := Tool("search", func(_ context.Context, in input) (string, error) {
		return in.Query, nil
	}, Description("search the corpus"))
	if err != nil {
		t.Fatalf("Tool() error = %v", err)
	}
	schema := tooltest.MustSchema(t, driver)
	if schema.Type != "object" {
		t.Fatalf("expected object schema, got %q", schema.Type)
	}
	if schema.Properties["query"].Description != "search query" {
		t.Fatalf("expected field description, got %q", schema.Properties["query"].Description)
	}
	result := tooltest.MustCall(t, driver, map[string]any{"query": "hydaelyn"})
	if result.Content != "hydaelyn" {
		t.Fatalf("unexpected content: %q", result.Content)
	}
}

func TestToolSupportsStreamingUpdates(t *testing.T) {
	type input struct {
		Name string `json:"name"`
	}
	driver, err := Tool("greeter", func(_ context.Context, in input, sink tool.UpdateSink) (map[string]string, error) {
		if err := sink(tool.Update{Kind: "progress", Message: "started"}); err != nil {
			return nil, err
		}
		return map[string]string{"message": "hello " + in.Name}, nil
	})
	if err != nil {
		t.Fatalf("Tool() error = %v", err)
	}
	updates := make([]tool.Update, 0, 1)
	result, err := driver.Execute(context.Background(), tool.Call{
		ID:        "call-1",
		Name:      "greeter",
		Arguments: json.RawMessage(`{"name":"mcp"}`),
	}, func(update tool.Update) error {
		updates = append(updates, update)
		return nil
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(updates) != 1 || updates[0].Kind != "progress" {
		t.Fatalf("unexpected updates: %#v", updates)
	}
	if result.Name != "greeter" {
		t.Fatalf("unexpected result name: %q", result.Name)
	}
}
