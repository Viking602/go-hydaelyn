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

	"github.com/Viking602/go-hydaelyn"
	"github.com/Viking602/go-hydaelyn/api"
)

func main() {
	ctx := context.Background()
	runner := hydaelyn.NewDevelopment()
	runner.RegisterAgent(api.AgentProfile{ID: "triage"})
	runner.RegisterAgent(api.AgentProfile{ID: "specialist"})

	run, _, err := runner.StartRun(ctx, api.StartRunCommand{Request: "investigate error spike"})
	must(err)
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID: run.ID, TaskID: "investigate", OwnerAgentID: "triage",
	})
	must(err)

	// Triage decides the task belongs to specialist before doing any work.
	must(runner.RequestHandoff(ctx, api.HandoffCommand{
		RunID: run.ID, TaskID: task.ID, TaskVersion: task.Version,
		FromAgentID: "triage", ToAgentID: "specialist",
		HandoffContext: "needs deep DB expertise",
	}))
	fmt.Println("handed off: triage → specialist")

	// Specialist receives a freshly-routed envelope and finishes the work.
	specEnv, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: "specialist",
	})
	must(err)
	specLease, _, err := runner.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{
		RunID: run.ID, TaskID: task.ID, EnvelopeID: specEnv.ID,
		HolderType: api.HolderAgent, HolderID: "specialist", TTL: time.Minute,
	})
	must(err)
	updated, err := runner.Task(ctx, run.ID, task.ID)
	must(err)
	must(runner.SubmitTypedReport(ctx, api.SubmitTypedReportCommand{
		RunID: run.ID, TaskID: task.ID, LeaseID: specLease.ID,
		HolderType: api.HolderAgent, HolderID: "specialist",
		TaskVersion: updated.Version,
		Report:      api.TypedReport{Status: api.ReportStatusSuccess, Summary: "root cause: missing index"},
	}))
	fmt.Printf("owner chain: %v → final owner=%s\n", updated.OwnerHistory, updated.OwnerAgentID)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
