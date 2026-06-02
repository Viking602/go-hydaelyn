package assertions_test

import (
	"context"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/eval/assertions"
)

func TestAssertion_WithinDuration_Pass(t *testing.T) {
	run, h := runToTerminal(t, "run-duration-ok", "x")
	if err := (assertions.WithinDuration{Max: time.Hour}).Check(context.Background(), run, h); err != nil {
		t.Fatalf("expected run within a generous duration bound, got %v", err)
	}
}

func TestAssertion_WithinDuration_TooSlowFails(t *testing.T) {
	run, h := runToTerminal(t, "run-duration-slow", "x")
	// Force a measurable span: the durable store keeps CreatedAt < UpdatedAt,
	// so any positive elapsed time exceeds a zero ceiling.
	if err := (assertions.WithinDuration{Max: 0}).Check(context.Background(), run, h); err == nil {
		// A zero-duration run technically satisfies a zero ceiling; only assert
		// failure when the store recorded a non-zero span.
		if run.UpdatedAt.Sub(run.CreatedAt) > 0 {
			t.Fatalf("expected over-duration run to fail with a zero ceiling")
		}
	}
}
