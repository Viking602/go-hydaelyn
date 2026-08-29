package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Viking602/venat/message"
)

type validationDriver struct {
	definition Definition
	calls      atomic.Int32
}

func (driver *validationDriver) Definition() Definition { return driver.definition }

func (driver *validationDriver) Execute(_ context.Context, call Call, _ UpdateSink) (Result, error) {
	driver.calls.Add(1)
	return Result{ToolCallID: call.ID, Name: call.Name, Content: "executed"}, nil
}

func TestBusRejectsInvalidArgumentsWithoutExecutingDriver(t *testing.T) {
	additional := false
	driver := &validationDriver{definition: Definition{
		Name: "lookup",
		InputSchema: Schema{
			Type: "object",
			Properties: map[string]Schema{
				"query": {Type: "string"},
			},
			Required:             []string{"query"},
			AdditionalProperties: &additional,
		},
	}}
	bus := NewBus(driver)
	for name, arguments := range map[string]json.RawMessage{
		"missing required":  json.RawMessage(`{}`),
		"wrong type":        json.RawMessage(`{"query":42}`),
		"unknown property":  json.RawMessage(`{"query":"ok","extra":true}`),
		"duplicate key":     json.RawMessage(`{"query":"first","query":"second"}`),
		"escaped duplicate": json.RawMessage(`{"query":"ok","\u0071uery":"again"}`),
	} {
		t.Run(name, func(t *testing.T) {
			result, err := bus.Execute(context.Background(), Call{ID: name, Name: "lookup", Arguments: arguments}, ExecuteOptions{})
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if !result.IsError || !strings.Contains(result.Content, "invalid tool arguments") {
				t.Fatalf("invalid argument result = %#v", result)
			}
		})
	}
	if driver.calls.Load() != 0 {
		t.Fatalf("driver executed %d invalid calls", driver.calls.Load())
	}
}

func TestBusExecutesSchemaValidArguments(t *testing.T) {
	driver := &validationDriver{definition: Definition{
		Name: "lookup",
		InputSchema: message.JSONSchema{
			Type:       "object",
			Properties: map[string]message.JSONSchema{"query": {Type: "string"}},
			Required:   []string{"query"},
		},
	}}
	result, err := NewBus(driver).Execute(context.Background(), Call{ID: "valid", Name: "lookup", Arguments: json.RawMessage(`{"query":"venat"}`)}, ExecuteOptions{})
	if err != nil || result.IsError || result.Content != "executed" || driver.calls.Load() != 1 {
		t.Fatalf("valid execution = %#v, calls=%d, err=%v", result, driver.calls.Load(), err)
	}
}

func TestBusFreezesDefinitionAndSchemaAtRegistration(t *testing.T) {
	driver := &validationDriver{definition: Definition{
		Name: "lookup",
		InputSchema: Schema{
			Type:       "object",
			Properties: map[string]Schema{"query": {Type: "string"}},
			Required:   []string{"query"},
		},
		Concurrency: ConcurrencySequential,
	}}
	bus := NewBus(driver)
	advertised := bus.Definitions()
	advertised[0].InputSchema.Properties["query"] = Schema{Type: "integer"}
	driver.definition.Name = "mutated"
	driver.definition.InputSchema.Properties["query"] = Schema{Type: "integer"}
	driver.definition.Concurrency = ConcurrencyParallel

	current := bus.Definitions()
	if len(current) != 1 || current[0].Name != "lookup" ||
		current[0].InputSchema.Properties["query"].Type != "string" ||
		current[0].Concurrency != ConcurrencySequential {
		t.Fatalf("registered definition mutated: %#v", current)
	}
	result, err := bus.Execute(context.Background(), Call{ID: "stable", Name: "lookup", Arguments: json.RawMessage(`{"query":"venat"}`)}, ExecuteOptions{})
	if err != nil || result.IsError || driver.calls.Load() != 1 {
		t.Fatalf("execution against frozen definition = %#v, calls=%d, err=%v", result, driver.calls.Load(), err)
	}
}

func TestBusSurfacesInvalidHostSchemaBeforeExecution(t *testing.T) {
	driver := &validationDriver{definition: Definition{
		Name:        "broken",
		InputSchema: Schema{Type: "not-a-json-schema-type"},
	}}
	_, err := NewBus(driver).Execute(context.Background(), Call{Name: "broken", Arguments: json.RawMessage(`{}`)}, ExecuteOptions{})
	if !errors.Is(err, ErrInvalidToolSchema) || driver.calls.Load() != 0 {
		t.Fatalf("invalid schema error = %v, calls=%d", err, driver.calls.Load())
	}
}

func TestBusRejectsDuplicateToolNamesWithoutReplacingOriginal(t *testing.T) {
	first := &validationDriver{definition: Definition{Name: "lookup", InputSchema: Schema{Type: "object"}}}
	second := &validationDriver{definition: Definition{Name: "lookup", InputSchema: Schema{Type: "object"}}}
	bus := NewBus(first, second)
	if err := bus.Validate(); !errors.Is(err, ErrDuplicateToolName) {
		t.Fatalf("duplicate bus validation error = %v", err)
	}

	dynamic := NewBus(first)
	if err := dynamic.Register(second); !errors.Is(err, ErrDuplicateToolName) {
		t.Fatalf("dynamic duplicate registration error = %v", err)
	}
	result, err := dynamic.Execute(context.Background(), Call{ID: "call", Name: "lookup", Arguments: json.RawMessage(`{}`)}, ExecuteOptions{})
	if err != nil || result.IsError || first.calls.Load() != 1 || second.calls.Load() != 0 {
		t.Fatalf("duplicate replacement result=%#v first=%d second=%d err=%v", result, first.calls.Load(), second.calls.Load(), err)
	}
}
