package research_test

import (
	"testing"

	"github.com/Viking602/venat/eval"
	"github.com/Viking602/venat/packs/research"
)

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

func TestResearchPack_Shape(t *testing.T) {
	if research.Pack.Name != research.PackName {
		t.Fatalf("pack name = %q, want %q", research.Pack.Name, research.PackName)
	}
	if len(research.Pack.Agents) != 3 {
		t.Fatalf("want three agents, got %d", len(research.Pack.Agents))
	}
}
