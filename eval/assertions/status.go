package assertions

import (
	"context"
	"fmt"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/eval"
)

// RunTerminatedWithStatus asserts that the run reached a specific terminal
// api.RunStatus (typically RunStatusCompleted, RunStatusFailed, or
// RunStatusCancelled).
type RunTerminatedWithStatus struct {
	// Status is the run status the run must have terminated with.
	Status api.RunStatus
}

// Name returns the assertion's stable identifier.
func (a RunTerminatedWithStatus) Name() string { return "RunTerminatedWithStatus" }

// Check reports whether run.Status equals the expected status.
func (a RunTerminatedWithStatus) Check(ctx context.Context, run api.Run, harness eval.Harness) error {
	if run.Status == a.Status {
		return nil
	}
	return fmt.Errorf("run terminated with status %q, want %q", run.Status, a.Status)
}

// EventEmitted asserts that at least one event of a given api.EventType was
// emitted during the run.
type EventEmitted struct {
	// Type is the event type that must have been emitted.
	Type api.EventType
}

// Name returns the assertion's stable identifier.
func (a EventEmitted) Name() string { return "EventEmitted" }

// Check scans the run's event stream for at least one event of the expected type.
func (a EventEmitted) Check(ctx context.Context, run api.Run, harness eval.Harness) error {
	runner := harness.Runner()
	if runner == nil {
		return fmt.Errorf("harness returned a nil runner")
	}
	events, err := runner.RunEvents(ctx, run.ID)
	if err != nil {
		return fmt.Errorf("read run events: %w", err)
	}
	for _, ev := range events {
		if ev.Type == a.Type {
			return nil
		}
	}
	return fmt.Errorf("no event of type %q was emitted", a.Type)
}
