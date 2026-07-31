package eval

import (
	"time"

	"github.com/Viking602/venat/api"
)

// EvalResult is the typed verdict for a single executed EvalCase.
type EvalResult struct {
	// Case is the EvalCase.Name.
	Case string
	// Run is the run as observed at terminal status (or the last observed
	// state when the case failed before reaching terminal).
	Run api.Run
	// Passed is true when every assertion held and the run executed without
	// a framework error.
	Passed bool
	// Failures lists every assertion that did not hold, plus any framework
	// error (setup/execution/timeout) reported under a synthetic name.
	Failures []AssertionFailure
	// Duration is the wall-clock time the case took to execute and grade.
	Duration time.Duration
	// Usage is a typed rollup of the run's metering records. It is the zero
	// value when no usage records are available.
	Usage UsageSummary
}

// UsageSummary is a typed rollup over the []api.UsageRecord captured during a
// run. It replaces the spec's phantom api.UsageSummary (§3) and carries no
// loose any fields (ADR-009).
type UsageSummary struct {
	// Records is the number of usage records summarized.
	Records int
	// InputTokens is the sum of UsageRecord.InputTokens.
	InputTokens int
	// OutputTokens is the sum of UsageRecord.OutputTokens.
	OutputTokens int
	// ToolCalls is the sum of UsageRecord.ToolCalls.
	ToolCalls int
	// Credits is the sum of UsageRecord.Credits.
	Credits int64
	// DurationMS is the sum of UsageRecord.DurationMS.
	DurationMS int64
}

// SummarizeUsage folds a slice of usage records into a typed UsageSummary.
func SummarizeUsage(records []api.UsageRecord) UsageSummary {
	var s UsageSummary
	s.Records = len(records)
	for _, r := range records {
		s.InputTokens += r.InputTokens
		s.OutputTokens += r.OutputTokens
		s.ToolCalls += r.ToolCalls
		s.Credits += r.Credits
		s.DurationMS += r.DurationMS
	}
	return s
}
