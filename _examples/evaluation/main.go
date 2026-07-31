// evaluation demonstrates the eval framework: declare an EvalCase with a
// scripted Harness and a few typed Assertions, then grade an executed run.
//
// In a real project this lives in a _test.go file and runs via
// eval.Run(t, c) / eval.RunSuite(t, cases). Because main() has no
// *testing.T, this example drives the same case through the Harness and
// grades each Assertion directly, printing the verdict.
//
//	go run ./_examples/evaluation
package main

import (
	"context"
	"fmt"

	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/eval"
	"github.com/Viking602/venat/eval/assertions"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/provider/scripted"
	"github.com/Viking602/venat/worker"
)

func main() {
	ctx := context.Background()

	// 1. Declare the case: a deterministic scripted model, the run to start,
	//    and the typed assertions that grade it.
	c := eval.EvalCase{
		Name:        "summary-quality",
		Description: "produce a concise summary",
		Setup: func() eval.Harness {
			return eval.NewHarness(eval.WithScript([]provider.Event{
				{Kind: provider.EventTextDelta, Text: "summary complete"},
				{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
			}))
		},
		Input: api.StartRunCommand{RunID: "eval-run", RootTaskID: "root", Request: "summarize a task"},
		Assertions: []eval.Assertion{
			assertions.RunTerminatedWithStatus{Status: api.RunStatusCompleted},
			assertions.OutputContains{Substring: "summary"},
			assertions.EventEmitted{Type: api.EventTaskCompleted},
		},
	}

	// 2. Build the harness and execute the run through the public surface.
	h := c.Setup()
	defer h.Cleanup()
	run := driveRun(ctx, h, c.Input)

	// 3. Grade each assertion. eval.Run(t, c) does exactly this under go test.
	passed := true
	for _, a := range c.Assertions {
		if err := a.Check(ctx, run, h); err != nil {
			passed = false
			fmt.Printf("FAIL %s: %v\n", a.Name(), err)
			continue
		}
		fmt.Printf("PASS %s\n", a.Name())
	}
	fmt.Printf("case %q passed=%v (run %s)\n", c.Name, passed, run.Status)
}

// driveRun starts the run, dispatches its task to the harness agent, runs the
// scripted agent loop via the worker bridge, and walks the run to completed —
// the same wiring eval's internal runner uses.
func driveRun(ctx context.Context, h eval.Harness, input api.StartRunCommand) api.Run {
	runner := h.Runner()
	dh := h.(*eval.DefaultHarness)

	run, _, err := runner.StartRun(ctx, input)
	must(err)
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       run.ID + "-task",
		Goal:         "summarize",
		OwnerAgentID: dh.AgentID(),
		WriteTargets: []string{"output"},
	})
	must(err)
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: dh.AgentID(),
	})
	must(err)

	engine := agent.Engine{Provider: scripted.New(dh.Script())}
	executor := worker.AgentWorker{Runner: runner, Engine: engine, AgentID: dh.AgentID(), Model: dh.Model()}
	must(executor.ExecuteEnvelope(ctx, worker.ExecuteEnvelopeRequest{Envelope: env}))

	_, err = runner.AdvanceRun(ctx, api.AdvanceRunCommand{RunID: run.ID})
	must(err)
	for _, to := range []api.RunStatus{api.RunStatusComposingResponse, api.RunStatusCompleted} {
		must(runner.TransitionRun(ctx, api.TransitionRunCommand{RunID: run.ID, To: to}))
	}
	final, err := runner.Run(ctx, run.ID)
	must(err)
	return final
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
