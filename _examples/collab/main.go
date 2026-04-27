// collab demonstrates handoff: agent A starts a task, decides B is the
// right owner, and transfers ownership via RequestHandoff. The framework
// tracks the owner-history chain and rejects cycles.
//
//	go run ./_examples/collab
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/Viking602/go-hydaelyn/orchestrator"
)

func main() {
	ctx := context.Background()
	rt := orchestrator.NewRuntime(orchestrator.Config{})
	rt.RegisterAgent(orchestrator.AgentProfile{ID: "triage"})
	rt.RegisterAgent(orchestrator.AgentProfile{ID: "specialist"})

	run, _, err := rt.StartRun(ctx, orchestrator.StartRunCommand{Request: "investigate error spike"})
	must(err)
	task, err := rt.CreateTask(ctx, orchestrator.CreateTaskCommand{
		RunID: run.ID, TaskID: "investigate", OwnerAgentID: "triage",
	})
	must(err)

	// Triage decides the task belongs to specialist before doing any work.
	must(rt.RequestHandoff(ctx, orchestrator.HandoffCommand{
		RunID: run.ID, TaskID: task.ID, TaskVersion: task.Version,
		FromAgentID: "triage", ToAgentID: "specialist",
		HandoffContext: "needs deep DB expertise",
	}))
	fmt.Println("handed off: triage → specialist")

	// Specialist receives a freshly-routed envelope and finishes the work.
	specEnv, err := rt.DispatchTask(ctx, orchestrator.DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: "specialist",
	})
	must(err)
	specLease, _, err := rt.AcquireTaskExecution(ctx, orchestrator.AcquireTaskExecutionCommand{
		RunID: run.ID, TaskID: task.ID, EnvelopeID: specEnv.ID,
		HolderType: orchestrator.HolderAgent, HolderID: "specialist", TTL: time.Minute,
	})
	must(err)
	updated, err := rt.Task(ctx, run.ID, task.ID)
	must(err)
	must(rt.SubmitTypedReport(ctx, orchestrator.SubmitTypedReportCommand{
		RunID: run.ID, TaskID: task.ID, LeaseID: specLease.ID,
		HolderType: orchestrator.HolderAgent, HolderID: "specialist",
		TaskVersion: updated.Version,
		Report:      orchestrator.TypedReport{Status: orchestrator.ReportStatusSuccess, Summary: "root cause: missing index"},
	}))
	fmt.Printf("owner chain: %v → final owner=%s\n", updated.OwnerHistory, updated.OwnerAgentID)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
