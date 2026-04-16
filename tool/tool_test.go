package tool

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/message"
)

type staticDriver struct {
	name string
}

func (d staticDriver) Definition() Definition {
	return Definition{
		Name: d.name,
		InputSchema: Schema{
			Type: "object",
		},
	}
}

func (d staticDriver) Execute(context.Context, Call, UpdateSink) (Result, error) {
	return Result{Name: d.name}, nil
}

func TestBusSubsetDefaultsToDenyByDefault(t *testing.T) {
	bus := NewBus(staticDriver{name: "alpha"}, staticDriver{name: "beta"})
	subset := bus.Subset(nil)
	if len(subset.Definitions()) != 0 {
		t.Fatalf("expected no tools when no names are granted, got %#v", subset.Definitions())
	}
}

func TestBusSubsetKeepsExplicitlyGrantedTools(t *testing.T) {
	bus := NewBus(staticDriver{name: "alpha"}, staticDriver{name: "beta"})
	subset := bus.Subset([]string{"beta"})
	definitions := subset.Definitions()
	if len(definitions) != 1 {
		t.Fatalf("expected one granted tool, got %#v", definitions)
	}
	if definitions[0].Name != "beta" {
		t.Fatalf("expected granted tool beta, got %#v", definitions[0])
	}
	if _, err := subset.Execute(context.Background(), Call{Name: "alpha"}, nil); err == nil {
		t.Fatalf("expected denied tool to be unavailable")
	}
	result, err := subset.Execute(context.Background(), Call{Name: "beta", Arguments: message.ToolCall{}.Arguments}, nil)
	if err != nil {
		t.Fatalf("expected granted tool to execute, got %v", err)
	}
	if result.Name != "beta" {
		t.Fatalf("unexpected tool result %#v", result)
	}
}
