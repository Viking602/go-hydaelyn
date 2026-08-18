package coding

import (
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/eval"
	"github.com/Viking602/venat/eval/assertions"
	"github.com/Viking602/venat/provider"
)

// SmokeCases is a wiring check: a scripted model narrates the hashline
// protocol so the pack's eval surface can grade a completed run.
//
// Deprecated: keep new suites in pack tests so production pack code does
// not need to grow more eval imports.
var SmokeCases = []eval.EvalCase{
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
