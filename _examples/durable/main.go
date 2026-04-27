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

	"github.com/Viking602/go-hydaelyn/orchestrator"
)

func main() {
	ctx := context.Background()
	rt := orchestrator.NewRuntime(orchestrator.Config{})
	rt.RegisterAgent(orchestrator.AgentProfile{ID: "worker"})

	run, _, err := rt.StartRun(ctx, orchestrator.StartRunCommand{Request: "recoverable run"})
	must(err)
	task, err := rt.CreateTask(ctx, orchestrator.CreateTaskCommand{
		RunID: run.ID, TaskID: "step-1", OwnerAgentID: "worker",
	})
	must(err)
	env, err := rt.DispatchTask(ctx, orchestrator.DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: "worker",
	})
	must(err)
	lease, _, err := rt.AcquireTaskExecution(ctx, orchestrator.AcquireTaskExecutionCommand{
		RunID: run.ID, TaskID: task.ID, EnvelopeID: env.ID,
		HolderType: orchestrator.HolderAgent, HolderID: "worker", TTL: time.Minute,
	})
	must(err)
	must(rt.SubmitTypedReport(ctx, orchestrator.SubmitTypedReportCommand{
		RunID: run.ID, TaskID: task.ID, LeaseID: lease.ID,
		HolderType: orchestrator.HolderAgent, HolderID: "worker",
		TaskVersion: task.Version,
		Report:      orchestrator.TypedReport{Status: orchestrator.ReportStatusSuccess, Summary: "done"},
	}))

	// Replay reconstructs the entire run state from events on disk/memory.
	projection, err := rt.ReplayRunState(run.ID)
	must(err)
	out, _ := json.MarshalIndent(projection, "", "  ")
	fmt.Println(string(out))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
