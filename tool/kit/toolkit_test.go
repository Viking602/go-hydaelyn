package kit

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/tool"
	"github.com/Viking602/venat/tool/tooltest"
)

func TestSchemaForRecursiveTypeDoesNotOverflow(t *testing.T) {
	type Node struct {
		Name string `json:"name"`
		Next *Node  `json:"next,omitempty"`
	}
	schema, err := schemaFor(reflect.TypeOf(Node{}))
	if err != nil {
		t.Fatalf("schemaFor() error = %v", err)
	}
	if schema.Type != "object" {
		t.Fatalf("schema type = %q, want object", schema.Type)
	}
}

func TestDecodeInputRequiresFields(t *testing.T) {
	type input struct {
		Query string `json:"query"`
	}
	if _, err := decodeInput(reflect.TypeOf(input{}), json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected missing required field error")
	}
}

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
	result := tooltest.MustCall(t, driver, map[string]any{"query": "venat"})
	if result.Content != "venat" {
		t.Fatalf("unexpected content: %q", result.Content)
	}
}

func TestToolCarriesExecutionSettings(t *testing.T) {
	type input struct {
		Target string `json:"target"`
	}
	driver, err := Tool("deploy", func(context.Context, input) (string, error) {
		return "ok", nil
	},
		Terminal(),
		Timeout(5*time.Second),
		Concurrency(tool.ConcurrencySequential),
		ConcurrencyGroup("deployments"),
		MaxConcurrency(1),
	)
	if err != nil {
		t.Fatalf("Tool() error = %v", err)
	}
	def := driver.Definition()
	if !def.Terminal || def.Timeout != 5*time.Second {
		t.Fatalf("terminal/timeout settings = %#v", def)
	}
	if def.Concurrency != tool.ConcurrencySequential || def.ConcurrencyGroup != "deployments" || def.MaxConcurrency != 1 {
		t.Fatalf("concurrency settings = %#v", def)
	}
}

func TestToolSupportsStreamingUpdates(t *testing.T) {
	type input struct {
		Name string `json:"name"`
	}
	driver, err := Tool("greeter", func(_ context.Context, in input, sink tool.UpdateSink) (map[string]string, error) {
		if err := sink(tool.Update{Kind: tool.UpdateProgress, Message: "started"}); err != nil {
			return nil, err
		}
		if err := sink(tool.Update{Kind: tool.UpdateOutput, Parts: []message.ContentPart{message.TextPart("hello " + in.Name)}}); err != nil {
			return nil, err
		}
		return map[string]string{"message": "hello " + in.Name}, nil
	})
	if err != nil {
		t.Fatalf("Tool() error = %v", err)
	}
	updates := make([]tool.Update, 0, 2)
	result, err := tool.NewBus(driver).Execute(context.Background(), tool.Call{
		ID:          "call-1",
		Name:        "greeter",
		OperationID: "turn:0:call:0",
		Arguments:   json.RawMessage(`{"name":"mcp"}`),
	}, tool.ExecuteOptions{Sink: func(update tool.Update) error {
		updates = append(updates, tool.CloneUpdate(update))
		return nil
	}})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(updates) != 2 || updates[0].Kind != tool.UpdateProgress || updates[1].Kind != tool.UpdateOutput {
		t.Fatalf("unexpected updates: %#v", updates)
	}
	for index, update := range updates {
		if update.ToolCallID != "call-1" || update.OperationID != "turn:0:call:0" || update.Sequence != uint64(index+1) {
			t.Fatalf("update[%d] identity = %#v", index, update)
		}
	}
	if result.Name != "greeter" || result.Content != "hello mcp" || len(result.Parts) != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
}
