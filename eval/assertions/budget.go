package assertions

import (
	"context"
	"fmt"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/eval"
)

// WithinBudget asserts that the run consumed no more than a credit ceiling.
// Credits are summed from the run's usage ledger (api.UsageStore) through the
// public Runner facade. A run that emitted no usage records spends zero credits
// and so always satisfies a non-negative ceiling.
type WithinBudget struct {
	// MaxCredits is the inclusive upper bound on summed usage credits.
	MaxCredits int64
}

// Name returns the assertion's stable identifier.
func (a WithinBudget) Name() string { return "WithinBudget" }

// Check reports whether the run's summed usage credits stay within MaxCredits.
func (a WithinBudget) Check(ctx context.Context, run api.Run, harness eval.Harness) error {
	runner := harness.Runner()
	if runner == nil {
		return fmt.Errorf("harness returned a nil runner")
	}
	uow, err := runner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin unit of work: %w", err)
	}
	credits, sumErr := uow.UsageRecords().SumCredits(ctx, api.UsageSelector{RunID: run.ID})
	// The query is read-only; roll the unit of work back so no state leaks.
	_ = uow.Rollback(ctx)
	if sumErr != nil {
		return fmt.Errorf("sum usage credits: %w", sumErr)
	}
	if credits > a.MaxCredits {
		return fmt.Errorf("run consumed %d credits, want at most %d", credits, a.MaxCredits)
	}
	return nil
}
