package assertions_test

import (
	"testing"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/eval"
)

// runToTerminal executes an assertion-free case to terminal status through the
// public eval runner and returns the terminal run plus the harness it ran in,
// so a test can write run state and then call an assertion's Check directly.
func runToTerminal(t *testing.T, runID, request string) (api.Run, eval.Harness) {
	t.Helper()
	var captured eval.Harness
	c := eval.EvalCase{
		Name: runID,
		Setup: func() eval.Harness {
			captured = eval.NewHarness(eval.WithScript(textScript("ok")))
			return captured
		},
		Input: api.StartRunCommand{RunID: runID, RootTaskID: "root", Request: request},
	}
	res := eval.Run(t, c)
	if !res.Passed {
		t.Fatalf("baseline run %q did not pass: %+v", runID, res.Failures)
	}
	return res.Run, captured
}

// runWithToolCall runs an assertion-free case to terminal status, then records
// a single tool-sourced blackboard item so tool assertions observe a call.
func runWithToolCall(t *testing.T, runID, tool, arg string) (api.Run, eval.Harness) {
	t.Helper()
	run, h := runToTerminal(t, runID, "drive")
	writeToolCall(t, h, runID, tool, arg)
	return run, h
}
