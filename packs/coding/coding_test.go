package coding_test

import (
	"testing"

	"github.com/Viking602/go-hydaelyn/eval"
	"github.com/Viking602/go-hydaelyn/packs/coding"
)

// TestCodingPack_SmokeSuite is the per-pack self-check: the pack ships its eval
// cases (coding.SmokeCases, also surfaced as Pack.EvalCases) and runs them
// through eval.RunSuite. Each case executes its scripted run to a terminal
// status and grades the typed assertions.
func TestCodingPack_SmokeSuite(t *testing.T) {
	results := eval.RunSuite(t, coding.SmokeCases)
	if len(results) != len(coding.SmokeCases) {
		t.Fatalf("RunSuite returned %d results, want %d", len(results), len(coding.SmokeCases))
	}
	for _, res := range results {
		if !res.Passed {
			t.Errorf("eval case %q failed: %+v", res.Case, res.Failures)
		}
	}
}

// TestCodingPack_Shape guards the manifest invariants the pack relies on: a
// named pack and manifest, one agent, and the full coding.* tool set surfaced
// as capabilities.
func TestCodingPack_Shape(t *testing.T) {
	if coding.Pack.Name != coding.PackName || coding.Pack.Version == "" {
		t.Fatalf("pack name/version: %q / %q", coding.Pack.Name, coding.Pack.Version)
	}
	if len(coding.Pack.Agents) != 1 {
		t.Fatalf("want exactly one agent, got %d", len(coding.Pack.Agents))
	}
	if len(coding.Pack.Capabilities) != 1 {
		t.Fatalf("want exactly one capability manifest, got %d", len(coding.Pack.Capabilities))
	}
	caps := coding.Pack.Capabilities[0].Capabilities
	if got, want := len(caps), 8; got != want {
		t.Fatalf("want %d coding capabilities, got %d", want, got)
	}
	for _, c := range caps {
		if c.Name == "" || c.EffectType == "" {
			t.Errorf("capability missing name/effect: %+v", c)
		}
	}
}
