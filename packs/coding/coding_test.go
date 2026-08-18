package coding_test

import (
	"testing"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/eval"
	"github.com/Viking602/venat/eval/assertions"
	"github.com/Viking602/venat/packs/coding"
	"github.com/Viking602/venat/provider"
)

// smokeCases is a wiring check: a scripted model narrates the hashline
// protocol so the pack's eval surface can grade a completed run. It is
// not a capability guard; the scripted model performs no real edit.
var smokeCases = []eval.EvalCase{
	{
		Name:        "hashline-protocol-narration-shape",
		Description: "scripted run completes and the eval surface observes the narrated output (wiring smoke check, not a capability guard)",
		Setup: func() eval.Harness {
			return eval.NewHarness(
				eval.WithAgentID("coding.code-editor"),
				eval.WithScript([]provider.Event{
					{Kind: provider.EventTextDelta, Text: "Read ¶main.go#A1B2, applied edit_hashline, verified with go_test."},
					{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
				}),
			)
		},
		Input: api.StartRunCommand{
			RunID:      "coding-smoke",
			RootTaskID: "root",
			Request:    "fix the off-by-one in main.go using the hashline protocol",
		},
		Assertions: []eval.Assertion{
			assertions.RunTerminatedWithStatus{Status: api.RunStatusCompleted},
			assertions.OutputContains{Substring: "edit_hashline"},
		},
	},
}

func TestCodingPack_SmokeSuite(t *testing.T) {
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
