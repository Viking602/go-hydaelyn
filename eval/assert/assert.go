// Package assert ships the four assertions the v0.8.0 eval framework
// considers "built-in": Contains, Equals, Regex, JudgedBy. They cover
// 90% of agent-grading scenarios; tests that need richer logic can
// implement eval.Assertion directly.
package assert

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/Viking602/go-hydaelyn/eval"
)

// Contains asserts that Outcome.FinalText contains the given substring
// after case-insensitive trim. Use it for "did the agent mention X?"
// style checks where exact wording isn't important.
type Contains struct {
	Needle        string
	CaseSensitive bool
	// Source picks which field of the Outcome to search. Defaults to
	// FinalText when "".
	Source string
}

// Name returns the assertion's stable identifier.
func (a Contains) Name() string {
	if a.Source != "" {
		return "contains:" + a.Source
	}
	return "contains"
}

// Check inspects the Outcome.
func (a Contains) Check(ctx context.Context, o eval.Outcome) eval.AssertionResult {
	hay := source(o, a.Source)
	needle := a.Needle
	if !a.CaseSensitive {
		hay = strings.ToLower(hay)
		needle = strings.ToLower(needle)
	}
	if strings.Contains(hay, needle) {
		return eval.AssertionResult{Passed: true, Detail: "matched"}
	}
	return eval.AssertionResult{Passed: false, Detail: fmt.Sprintf("substring %q not found in %s", a.Needle, displaySource(a.Source))}
}

// Equals asserts that Outcome.FinalText equals the given string after
// trimming surrounding whitespace.
type Equals struct {
	Want   string
	Source string
}

// Name returns the assertion's stable identifier.
func (a Equals) Name() string { return "equals" }

// Check inspects the Outcome.
func (a Equals) Check(ctx context.Context, o eval.Outcome) eval.AssertionResult {
	got := strings.TrimSpace(source(o, a.Source))
	want := strings.TrimSpace(a.Want)
	if got == want {
		return eval.AssertionResult{Passed: true, Detail: "equal"}
	}
	return eval.AssertionResult{Passed: false, Detail: fmt.Sprintf("want %q, got %q", want, got)}
}

// Regex asserts that Outcome.FinalText matches the given pattern.
type Regex struct {
	Pattern string
	Source  string
}

// Name returns the assertion's stable identifier.
func (a Regex) Name() string { return "regex" }

// Check inspects the Outcome.
func (a Regex) Check(ctx context.Context, o eval.Outcome) eval.AssertionResult {
	re, err := regexp.Compile(a.Pattern)
	if err != nil {
		return eval.AssertionResult{Passed: false, Detail: "invalid regex: " + err.Error()}
	}
	hay := source(o, a.Source)
	if re.MatchString(hay) {
		return eval.AssertionResult{Passed: true, Detail: "matched"}
	}
	return eval.AssertionResult{Passed: false, Detail: fmt.Sprintf("pattern %q did not match", a.Pattern)}
}

// JudgedBy delegates the verdict to a caller-supplied function. Use it
// for LLM-as-judge graders, golden-file diffs, or any custom logic that
// won't fit the simpler assertions. The judge function receives the
// outcome and returns (passed, detail).
type JudgedBy struct {
	Label string
	Judge func(ctx context.Context, o eval.Outcome) (passed bool, detail string)
}

// Name returns the assertion's stable identifier. Defaults to "judged"
// when no Label is set.
func (a JudgedBy) Name() string {
	if a.Label != "" {
		return "judged:" + a.Label
	}
	return "judged"
}

// Check inspects the Outcome.
func (a JudgedBy) Check(ctx context.Context, o eval.Outcome) eval.AssertionResult {
	if a.Judge == nil {
		return eval.AssertionResult{Passed: false, Detail: "no judge function provided"}
	}
	passed, detail := a.Judge(ctx, o)
	return eval.AssertionResult{Passed: passed, Detail: detail}
}

// source extracts the requested field from the Outcome. Supported
// values: "final" (default), "request", "artifact". Unknown values fall
// back to FinalText so authors can extend Source later without breaking
// existing cases.
func source(o eval.Outcome, name string) string {
	switch strings.ToLower(name) {
	case "", "final", "final_text", "output":
		return o.FinalText
	case "request":
		return o.Run.Request
	case "artifact":
		return o.ProducedArtifact
	default:
		return o.FinalText
	}
}

func displaySource(name string) string {
	if name == "" {
		return "FinalText"
	}
	return name
}
