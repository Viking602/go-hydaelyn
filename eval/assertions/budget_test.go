package assertions_test

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/eval"
	"github.com/Viking602/go-hydaelyn/eval/assertions"
)

// appendUsage records a usage credit datum on the run's ledger through the
// public unit-of-work surface so WithinBudget observes it.
func appendUsage(t *testing.T, h eval.Harness, runID string, credits int64) {
	t.Helper()
	ctx := context.Background()
	uow, err := h.Runner().Begin(ctx)
	if err != nil {
		t.Fatalf("Begin error = %v", err)
	}
	if err := uow.UsageRecords().AppendUsage(ctx, api.UsageRecord{
		ID:      runID + "-usage",
		RunID:   runID,
		Credits: credits,
	}); err != nil {
		t.Fatalf("AppendUsage error = %v", err)
	}
	if err := uow.Commit(ctx); err != nil {
		t.Fatalf("Commit error = %v", err)
	}
}

func TestAssertion_WithinBudget_NoUsage(t *testing.T) {
	run, h := runToTerminal(t, "run-budget-zero", "x")
	if err := (assertions.WithinBudget{MaxCredits: 100}).Check(context.Background(), run, h); err != nil {
		t.Fatalf("expected zero-usage run within budget, got %v", err)
	}
}

func TestAssertion_WithinBudget_UnderCeiling(t *testing.T) {
	run, h := runToTerminal(t, "run-budget-under", "x")
	appendUsage(t, h, run.ID, 40)
	if err := (assertions.WithinBudget{MaxCredits: 100}).Check(context.Background(), run, h); err != nil {
		t.Fatalf("expected within budget, got %v", err)
	}
}

func TestAssertion_WithinBudget_OverCeilingFails(t *testing.T) {
	run, h := runToTerminal(t, "run-budget-over", "x")
	appendUsage(t, h, run.ID, 250)
	if err := (assertions.WithinBudget{MaxCredits: 100}).Check(context.Background(), run, h); err == nil {
		t.Fatalf("expected over-budget run to fail")
	}
}
