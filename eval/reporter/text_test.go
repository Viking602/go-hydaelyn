package reporter_test

import (
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/eval/reporter"
)

func TestReporter_Text_FailuresAndTotals(t *testing.T) {
	out := reporter.Text{}.Render(sampleResults())

	if strings.Contains(out, "passing-case") {
		t.Errorf("default Text should omit passing cases, got:\n%s", out)
	}
	if !strings.Contains(out, "FAIL failing-case") {
		t.Errorf("expected a FAIL line for the failing case, got:\n%s", out)
	}
	for _, want := range []string{
		"OutputContains:",
		"RunTerminatedWithStatus:",
		"1 passed, 1 failed, 2 total",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got:\n%s", want, out)
		}
	}
}

func TestReporter_Text_ShowPassing(t *testing.T) {
	out := reporter.Text{ShowPassing: true}.Render(sampleResults())
	if !strings.Contains(out, "PASS passing-case") {
		t.Errorf("ShowPassing should print passing cases, got:\n%s", out)
	}
}

func TestReporter_Text_WriteMatchesRender(t *testing.T) {
	r := reporter.Text{ShowPassing: true}
	var b strings.Builder
	n, err := r.Write(&b, sampleResults())
	if err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	if got := r.Render(sampleResults()); b.String() != got {
		t.Errorf("Write output differs from Render output")
	}
	if n != len(b.String()) {
		t.Errorf("Write returned n=%d, want %d", n, len(b.String()))
	}
}
