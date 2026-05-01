// worker demonstrates the optional AgentWorker glue.
//
//	go run ./_examples/worker
package main

import (
	"context"
	"fmt"

	"github.com/Viking602/go-hydaelyn"
	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/provider/scripted"
	"github.com/Viking602/go-hydaelyn/worker"
)

func main() {
	ctx := context.Background()
	runner := hydaelyn.New()
	runner.RegisterAgent(hydaelyn.AgentProfile{ID: "agent-a"})

	run, _, err := runner.StartRun(ctx, hydaelyn.StartRunCommand{Request: "summarize a task"})
	must(err)
	task, err := runner.CreateTask(ctx, hydaelyn.CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "summary",
		Goal:         "produce a concise summary",
		OwnerAgentID: "agent-a",
		WriteTargets: []string{"summary"},
	})
	must(err)
	env, err := runner.DispatchTask(ctx, hydaelyn.DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a",
	})
	must(err)

	engine := agent.Engine{Provider: scripted.New([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "summary complete"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	})}
	executor := worker.AgentWorker{Runner: runner, Engine: engine, AgentID: "agent-a", Model: "scripted"}
	must(executor.ExecuteEnvelope(ctx, worker.ExecuteEnvelopeRequest{Envelope: env}))

	completed, err := runner.Task(ctx, run.ID, task.ID)
	must(err)
	fmt.Println(completed.Status, completed.Result.Summary)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
