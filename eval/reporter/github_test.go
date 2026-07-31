package reporter_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/eval"
	"github.com/Viking602/venat/eval/reporter"
)

func TestReporter_GitHub_ErrorAnnotationsAndNotice(t *testing.T) {
	out := reporter.GitHub{Title: "evalsuite"}.Render(sampleResults())
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	if len(lines) != 3 {
		t.Fatalf("expected 2 error lines + 1 notice, got %d lines:\n%s", len(lines), out)
	}
	for _, l := range lines[:2] {
		if !strings.HasPrefix(l, "::error ") {
			t.Errorf("expected an ::error annotation, got %q", l)
		}
	}
	notice := lines[2]
	if !strings.HasPrefix(notice, "::notice ") {
		t.Errorf("expected a trailing ::notice line, got %q", notice)
	}
	if !strings.HasSuffix(notice, "::1 passed, 1 failed, 2 total") {
		t.Errorf("notice missing totals, got %q", notice)
	}
}

func TestReporter_GitHub_EscapesDelimiters(t *testing.T) {
	results := []eval.EvalResult{
		{
			// Case name and assertion are property values (title=...), so their
			// ":" and "," must be percent-encoded; the detail is the data
			// segment, where only "%", CR, and LF are escaped.
			Case:   "case: with, specials",
			Run:    api.Run{ID: "r"},
			Passed: false,
			Failures: []eval.AssertionFailure{
				{Assertion: "Tricky", Detail: "line one\nline: two, %done"},
			},
			Duration: time.Millisecond,
		},
	}
	out := reporter.GitHub{}.Render(results)

	errLine := strings.SplitN(out, "\n", 2)[0]
	dataIdx := strings.LastIndex(errLine, "::")
	if dataIdx < 0 {
		t.Fatalf("malformed annotation: %q", errLine)
	}
	title, data := errLine[:dataIdx], errLine[dataIdx+2:]

	// The data segment must not contain a raw newline, which would prematurely
	// terminate the workflow command, but its ":" / "," stay literal.
	if strings.ContainsAny(data, "\n\r") {
		t.Errorf("escaped data still contains a raw newline: %q", data)
	}
	for _, enc := range []string{"%0A", "%25"} {
		if !strings.Contains(data, enc) {
			t.Errorf("expected encoded sequence %s in data segment, got:\n%s", enc, data)
		}
	}
	// Property delimiters in the title segment are encoded.
	for _, enc := range []string{"%3A", "%2C"} {
		if !strings.Contains(title, enc) {
			t.Errorf("expected encoded sequence %s in title segment, got:\n%s", enc, title)
		}
	}
}

func TestReporter_GitHub_AllPassingOnlyNotice(t *testing.T) {
	results := []eval.EvalResult{
		{Case: "ok", Passed: true, Duration: time.Millisecond},
	}
	out := reporter.GitHub{}.Render(results)
	if strings.Contains(out, "::error") {
		t.Errorf("no error annotations expected for an all-pass suite, got:\n%s", out)
	}
	if !strings.HasPrefix(out, "::notice ") {
		t.Errorf("expected only a notice line, got:\n%s", out)
	}
}
