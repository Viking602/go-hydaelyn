package research_test

import (
	"testing"

	"github.com/Viking602/go-hydaelyn/eval"
	"github.com/Viking602/go-hydaelyn/packs/research"
)

// TestResearchPack_SmokeSuite is the canonical per-pack self-check: a pack
// ships its eval cases (research.SmokeCases, also surfaced as Pack.EvalCases)
// and a host runs them through eval.RunSuite(t, cases). Each case executes its
// scripted run to a terminal status and grades the typed assertions; a failing
// assertion fails the corresponding subtest. Swapping the harness's scripted
// provider for a live one turns this smoke suite into a quality gate without
// changing the case shape.
func TestResearchPack_SmokeSuite(t *testing.T) {
	results := eval.RunSuite(t, research.SmokeCases)
	if len(results) != len(research.SmokeCases) {
		t.Fatalf("RunSuite returned %d results, want %d", len(results), len(research.SmokeCases))
	}
	for _, res := range results {
		if !res.Passed {
			t.Errorf("eval case %q failed: %+v", res.Case, res.Failures)
		}
	}
}
