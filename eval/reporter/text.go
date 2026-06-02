// Package reporter renders a slice of eval.EvalResult into the formats CI and
// humans consume: JUnit XML (junit.go), GitHub Checks annotations (github.go),
// and a terminal-friendly summary (text.go). Each reporter turns results into
// bytes (or writes them to an io.Writer) so a single suite run can be fanned
// out to every format without re-executing it.
//
// reporter imports the eval package to read EvalResult; eval never imports
// reporter, so the dependency stays acyclic (plan §4).
package reporter

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/eval"
)

// Text renders results as a terminal-friendly summary: one PASS/FAIL line per
// case, an indented detail line per failed assertion, and a trailing totals
// line. It is deliberately plain text (no ANSI color) so it reads cleanly in
// CI logs and local terminals alike.
type Text struct {
	// ShowPassing, when true, prints a line for passing cases too. When false
	// (the default) only failing cases and the totals line are written, which
	// keeps large green suites quiet.
	ShowPassing bool
}

// Render returns the summary as a string.
func (r Text) Render(results []eval.EvalResult) string {
	var b strings.Builder
	r.write(&b, results)
	return b.String()
}

// Write renders the summary to w. It returns the number of bytes written and
// any write error.
func (r Text) Write(w io.Writer, results []eval.EvalResult) (int, error) {
	return io.WriteString(w, r.Render(results))
}

func (r Text) write(b *strings.Builder, results []eval.EvalResult) {
	var passed, failed int
	for _, res := range results {
		if res.Passed {
			passed++
			if r.ShowPassing {
				fmt.Fprintf(b, "PASS %s (%s)\n", res.Case, formatDuration(res.Duration))
			}
			continue
		}
		failed++
		fmt.Fprintf(b, "FAIL %s (%s)\n", res.Case, formatDuration(res.Duration))
		for _, f := range res.Failures {
			fmt.Fprintf(b, "    %s: %s\n", f.Assertion, f.Detail)
		}
	}
	fmt.Fprintf(b, "%d passed, %d failed, %d total\n", passed, failed, passed+failed)
}

// formatDuration renders a duration with millisecond precision so the summary
// is stable across runs of short deterministic cases.
func formatDuration(d time.Duration) string {
	return d.Round(time.Millisecond).String()
}
