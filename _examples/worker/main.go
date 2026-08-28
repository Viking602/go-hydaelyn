// worker demonstrates the optional AgentWorker glue.
//
//	go run ./_examples/worker
package main

import (
	"context"
	"fmt"

	"github.com/Viking602/venat"
	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/provider/scripted"
	"github.com/Viking602/venat/worker"
)

func main() {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	must(runner.RegisterAgent(api.AgentProfile{ID: "agent-a"}))

	run, _, err := runner.StartRun(ctx, api.StartRunCommand{Request: "summarize a task"})
	must(err)
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "summary",
		Goal:         "produce a concise summary",
		OwnerAgentID: "agent-a",
		WriteTargets: []string{"summary"},
	})
	must(err)
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a",
	})
	must(err)

	engine := agent.Engine{Provider: scripted.New([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "summary complete"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	})}
	executor := worker.AgentWorker{Runner: runner, Engine: engine, AgentID: "agent-a", Model: "scripted"}
	_, err = executor.ExecuteEnvelope(ctx, worker.ExecuteEnvelopeRequest{Envelope: env})
	must(err)

	completed, err := runner.Task(ctx, run.ID, task.ID)
	must(err)
	fmt.Println(completed.Status, completed.Result.Summary)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
