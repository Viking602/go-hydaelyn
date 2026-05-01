// durable demonstrates event-sourced replay: after a run completes,
// ReplayRunState rebuilds the full projection from the event log alone.
// This is the foundation for crash recovery and audit.
//
//	go run ./_examples/durable
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	hydaelyn "github.com/Viking602/go-hydaelyn"
)

func main() {
	ctx := context.Background()
	runner := hydaelyn.New()
	runner.RegisterAgent(hydaelyn.AgentProfile{ID: "worker"})

	run, _, err := runner.StartRun(ctx, hydaelyn.StartRunCommand{Request: "recoverable run"})
	must(err)
	task, err := runner.CreateTask(ctx, hydaelyn.CreateTaskCommand{
		RunID: run.ID, TaskID: "step-1", OwnerAgentID: "worker",
	})
	must(err)
	env, err := runner.DispatchTask(ctx, hydaelyn.DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: "worker",
	})
	must(err)
	lease, _, err := runner.AcquireTaskExecution(ctx, hydaelyn.AcquireTaskExecutionCommand{
		RunID: run.ID, TaskID: task.ID, EnvelopeID: env.ID,
		HolderType: hydaelyn.HolderAgent, HolderID: "worker", TTL: time.Minute,
	})
	must(err)
	must(runner.SubmitTypedReport(ctx, hydaelyn.SubmitTypedReportCommand{
		RunID: run.ID, TaskID: task.ID, LeaseID: lease.ID,
		HolderType: hydaelyn.HolderAgent, HolderID: "worker",
		TaskVersion: task.Version,
		Report:      hydaelyn.TypedReport{Status: hydaelyn.ReportStatusSuccess, Summary: "done"},
	}))

	// Replay reconstructs the entire run state from events on disk/memory.
	projection, err := runner.ReplayRunState(run.ID)
	must(err)
	out, _ := json.MarshalIndent(projection, "", "  ")
	fmt.Println(string(out))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
