// Package eval is the v0.8.0 minimal assertion framework for grading
// agent runs. It is not a benchmark harness — it is the small layer that
// turns "did this Run produce the right outputs?" into a typed verdict
// you can ship in CI and compare across runs.
//
// The framework owns four pieces:
//
//   - Case: a single named scenario carrying inputs + Assertions.
//   - Assertion: a unit predicate over a Run/Task/Blackboard outcome.
//   - Suite: a named set of Cases that share setup.
//   - Result: typed verdict (pass/fail/error) plus per-assertion detail.
//
// Concrete assertions ship in eval/assert (Contains, Equals, Regex,
// JudgedBy). External graders plug in by satisfying Assertion directly.
//
// The grader does NOT execute the agent run; callers run the Case
// (typically via a Runner.StartRun) and pass the resulting Outcome into
// Eval. This keeps eval/ free of provider/runner dependencies, so it
// can be used inside CI containers without a model key.
package eval

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/api"
)

// Outcome bundles the artifacts an Assertion may want to inspect. Tests
// fill the subset they produced — empty fields just yield assertions
// that don't match on those dimensions.
type Outcome struct {
	Run              api.Run
	Tasks            []api.Task
	Events           []api.Event
	BlackboardItems  []api.BlackboardItem
	UserMessages     []api.UserMessage
	FinalText        string
	UsageRecords     []api.UsageRecord
	ActionAttempts   []api.ActionAttempt
	DurationSeconds  float64
	ProducedArtifact string
}

// Assertion is the unit grader. Each assertion returns a single
// AssertionResult; multiple assertions compose into a CaseResult.
type Assertion interface {
	// Name returns a stable identifier for this assertion. Used in test
	// output and CI diffing — keep it short and lowercase.
	Name() string
	// Check inspects o and returns whether the assertion passed plus a
	// human-readable detail string (typically the diff or value seen).
	Check(ctx context.Context, o Outcome) AssertionResult
}

// AssertionResult is the typed verdict of a single Assertion.Check call.
type AssertionResult struct {
	Name    string
	Passed  bool
	Detail  string
	Elapsed time.Duration
}

// Case is one named scenario in a Suite. Input/Setup are caller-managed:
// the eval framework does not execute the run, it only grades the result.
type Case struct {
	Name        string
	Description string
	Tags        []string
	Assertions  []Assertion
}

// CaseResult is the per-case verdict produced by Eval.
type CaseResult struct {
	CaseName   string
	Passed     bool
	Elapsed    time.Duration
	Assertions []AssertionResult
	Error      string
}

// Suite is a named bundle of Cases that share grading semantics. Suites
// don't own configuration today; they exist so reports and CLI output
// can group cases without inventing ad-hoc naming.
type Suite struct {
	Name        string
	Description string
	Cases       []Case
}

// SuiteResult is the rollup verdict produced by Run(Suite, ...).
type SuiteResult struct {
	SuiteName string
	Passed    bool
	Cases     []CaseResult
	StartedAt time.Time
	EndedAt   time.Time
}

// Eval runs every Assertion on a single Outcome and returns the
// aggregated CaseResult. Assertions run in declaration order; the
// CaseResult.Passed flag is the AND of every AssertionResult.Passed.
func Eval(ctx context.Context, c Case, o Outcome) CaseResult {
	start := time.Now()
	out := CaseResult{CaseName: c.Name, Passed: true}
	for _, a := range c.Assertions {
		t0 := time.Now()
		r := a.Check(ctx, o)
		if r.Name == "" {
			r.Name = a.Name()
		}
		r.Elapsed = time.Since(t0)
		out.Assertions = append(out.Assertions, r)
		if !r.Passed {
			out.Passed = false
		}
	}
	out.Elapsed = time.Since(start)
	return out
}

// Run executes Eval across every case in s using the provided outcome
// supplier — usually a closure that calls Runner.StartRun and packs the
// result into an Outcome. The supplier may return an error to mark the
// case as a grading error (distinct from an assertion failure).
func Run(ctx context.Context, s Suite, supply func(ctx context.Context, c Case) (Outcome, error)) SuiteResult {
	start := time.Now().UTC()
	out := SuiteResult{SuiteName: s.Name, Passed: true, StartedAt: start}
	for _, c := range s.Cases {
		o, err := supply(ctx, c)
		var cr CaseResult
		if err != nil {
			cr = CaseResult{
				CaseName: c.Name,
				Passed:   false,
				Error:    fmt.Sprintf("setup: %v", err),
			}
		} else {
			cr = Eval(ctx, c, o)
		}
		out.Cases = append(out.Cases, cr)
		if !cr.Passed {
			out.Passed = false
		}
	}
	out.EndedAt = time.Now().UTC()
	return out
}

// SummaryLine returns a one-line CI-friendly description of a
// SuiteResult: "suite-name: 4/5 cases passed (1 failed)".
func (r SuiteResult) SummaryLine() string {
	passed := 0
	for _, c := range r.Cases {
		if c.Passed {
			passed++
		}
	}
	total := len(r.Cases)
	failed := total - passed
	return fmt.Sprintf("%s: %d/%d cases passed (%d failed)", r.SuiteName, passed, total, failed)
}

// FailedAssertions returns the (case, assertion) pairs that failed.
// Useful for printing a compact failure report in CI logs.
func (r SuiteResult) FailedAssertions() []FailureRow {
	var rows []FailureRow
	for _, c := range r.Cases {
		for _, a := range c.Assertions {
			if a.Passed {
				continue
			}
			rows = append(rows, FailureRow{
				CaseName:      c.CaseName,
				AssertionName: a.Name,
				Detail:        strings.TrimSpace(a.Detail),
			})
		}
		if c.Error != "" {
			rows = append(rows, FailureRow{CaseName: c.CaseName, AssertionName: "setup", Detail: c.Error})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CaseName != rows[j].CaseName {
			return rows[i].CaseName < rows[j].CaseName
		}
		return rows[i].AssertionName < rows[j].AssertionName
	})
	return rows
}

// FailureRow is a single line in the failure report.
type FailureRow struct {
	CaseName      string
	AssertionName string
	Detail        string
}
