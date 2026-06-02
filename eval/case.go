package eval

import (
	"context"
	"time"

	"github.com/Viking602/go-hydaelyn/api"
)

// EvalCase is one named scenario the framework executes and grades. Setup
// builds the Harness the case runs in; Input is the run the framework starts;
// Assertions grade the resulting api.Run.
type EvalCase struct {
	// Name is the stable identifier for the case, used in reports and CI output.
	Name string
	// Description is a human-readable summary of what the case verifies.
	Description string
	// Setup returns the Harness the case executes in. The framework calls it
	// once per run and calls Harness.Cleanup afterwards. When nil, the
	// framework uses NewHarness with the case's defaults. Setup takes no
	// *testing.T so packs can declare EvalCases as plain values without
	// importing the testing package into their production build; the testing
	// dependency stays confined to the Run/RunSuite wrappers (plan §6.2).
	Setup func() Harness
	// Input is the command used to start the run. (Spec §3: api.StartRunCommand
	// replaces the spec's phantom api.RunInput.)
	Input api.StartRunCommand
	// Timeout bounds how long the run may take before the case is marked failed.
	// Zero means no timeout.
	Timeout time.Duration
	// Assertions are graded against the executed run in declaration order.
	Assertions []Assertion
}

// Assertion is a typed predicate over an executed run. Check returns nil when
// the assertion holds, or a non-nil error describing the failure. The error's
// message becomes the AssertionFailure.Detail.
type Assertion interface {
	// Name returns a stable identifier for the assertion, used in reports.
	Name() string
	// Check inspects the executed run (and, when needed, the Harness it ran in)
	// and returns nil on success or an error describing the failure.
	Check(ctx context.Context, run api.Run, harness Harness) error
}

// AssertionFailure records a single failed assertion within an EvalResult.
type AssertionFailure struct {
	// Assertion is the failing assertion's Name.
	Assertion string
	// Detail is the failure message returned by Assertion.Check.
	Detail string
}
