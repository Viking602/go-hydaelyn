package research

import (
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/eval"
	"github.com/Viking602/venat/eval/assertions"
	"github.com/Viking602/venat/provider"
)

// SmokeCases is a one-case eval suite that drives the pack against a
// deterministic scripted model. Hosts that still call
// eval.RunSuite(t, research.SmokeCases) keep compiling.
//
// Deprecated: keep new suites in pack tests so production pack code does
// not need to grow more eval imports.
var SmokeCases = []eval.EvalCase{
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
