package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/venat/tool"
)

var errParallelToolFailure = errors.New("parallel tool failed")

type partialParallelDriver struct {
	name string
	err  error
}

func (d partialParallelDriver) Definition() tool.Definition {
	return tool.Definition{Name: d.name}
}

func (d partialParallelDriver) Execute(_ context.Context, _ tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	if d.err != nil {
		return tool.Result{}, d.err
	}
	// Leave result identity empty to exercise the bus/engine fallback contract.
	return tool.Result{Content: "completed"}, nil
}

// TestDispatchPreparedToolsPreservesParallelResultIdentityAfterEarlierFailure
// guards durable recovery against replaying a successful side effect. Parallel
// execution compacts successful results when another slot fails, so every
// surviving result must retain the identity of its original call slot.
func TestDispatchPreparedToolsPreservesParallelResultIdentityAfterEarlierFailure(t *testing.T) {
	engine := Engine{Tools: tool.NewBus(
		partialParallelDriver{name: "first", err: errParallelToolFailure},
		partialParallelDriver{name: "second"},
	)}

	results, err := engine.dispatchPreparedTools(context.Background(), []tool.Call{
		{ID: "call-1", Name: "first"},
		{ID: "call-2", Name: "second"},
	}, tool.ModeParallel, nil)

	if !errors.Is(err, errParallelToolFailure) {
		t.Fatalf("dispatchPreparedTools() error = %v, want %v", err, errParallelToolFailure)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if result := results[0]; result.ToolCallID != "call-2" || result.Name != "second" {
		t.Fatalf("surviving result identity = (%q, %q), want (%q, %q)", result.ToolCallID, result.Name, "call-2", "second")
	}
}
