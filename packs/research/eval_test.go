package research_test

import (
	"testing"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/eval"
	"github.com/Viking602/venat/eval/assertions"
	"github.com/Viking602/venat/packs/research"
	"github.com/Viking602/venat/provider"
)

var smokeCases = []eval.EvalCase{
	{
		Name:        "cited-answer",
		Description: "the final answer is non-empty and mentions a source",
		Setup: func() eval.Harness {
			return eval.NewHarness(eval.WithScript([]provider.Event{
				{Kind: provider.EventTextDelta, Text: "Answer with source [1] attached."},
				{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
			}))
		},
		Input: api.StartRunCommand{
			RunID:      "research-smoke",
			RootTaskID: "root",
			Request:    "summarize the question with at least one source",
		},
		Assertions: []eval.Assertion{
			assertions.RunTerminatedWithStatus{Status: api.RunStatusCompleted},
			assertions.OutputContains{Substring: "source"},
		},
	},
}

func TestResearchPack_SmokeSuite(t *testing.T) {
	results := eval.RunSuite(t, smokeCases)
	if len(results) != len(smokeCases) {
		t.Fatalf("RunSuite returned %d results, want %d", len(results), len(smokeCases))
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
