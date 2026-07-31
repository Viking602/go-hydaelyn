package assertions

import (
	"context"
	"fmt"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/eval"
)

// WithinDuration asserts that the run completed within a wall-clock bound. The
// run's duration is measured from its CreatedAt to its UpdatedAt timestamps,
// which the durable store maintains across the run lifecycle.
type WithinDuration struct {
	// Max is the inclusive upper bound on the run's wall-clock duration.
	Max time.Duration
}

// Name returns the assertion's stable identifier.
func (a WithinDuration) Name() string { return "WithinDuration" }

// Check reports whether the run's CreatedAt..UpdatedAt span stays within Max.
func (a WithinDuration) Check(ctx context.Context, run api.Run, harness eval.Harness) error {
	if run.UpdatedAt.IsZero() || run.CreatedAt.IsZero() {
		return fmt.Errorf("run is missing timestamps to measure duration")
	}
	elapsed := run.UpdatedAt.Sub(run.CreatedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed > a.Max {
		return fmt.Errorf("run took %s, want at most %s", elapsed, a.Max)
	}
	return nil
}
