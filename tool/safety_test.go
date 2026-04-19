package tool

import (
	"context"
	"errors"
	"testing"
)

type safetyDriver struct {
	name  string
	calls int
	text  string
}

func (d *safetyDriver) Definition() Definition {
	return Definition{Name: d.name, InputSchema: Schema{Type: "object"}}
}

func (d *safetyDriver) Execute(context.Context, Call, UpdateSink) (Result, error) {
	d.calls++
	return Result{Name: d.name, Content: d.text}, nil
}

func TestUnsafeToolSelection(t *testing.T) {
	t.Parallel()

	safe := &safetyDriver{name: "safe", text: "ok"}
	dangerous := &safetyDriver{name: "dangerous", text: "boom"}
	bus := NewBus(safe, dangerous)
	restricted := bus.Subset([]string{"safe"})

	if _, err := restricted.Execute(context.Background(), Call{Name: "dangerous"}, nil); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("expected dangerous tool to be blocked, got %v", err)
	}
	if dangerous.calls != 0 {
		t.Fatalf("expected dangerous tool to remain uncalled, got %d", dangerous.calls)
	}
	if _, err := restricted.ExecuteBatch(context.Background(), []Call{{Name: "safe"}, {Name: "dangerous"}}, ModeSequential, nil); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("expected mixed misuse batch to fail, got %v", err)
	}
	if safe.calls != 1 {
		t.Fatalf("expected safe tool to run once before denial, got %d", safe.calls)
	}
	if dangerous.calls != 0 {
		t.Fatalf("expected dangerous tool misuse to stay blocked, got %d", dangerous.calls)
	}
}
