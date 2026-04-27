// Recipe: collab — multi-branch parallel work where each branch hands off
// from implementer to reviewer. The synthesizer waits for any 2 branches
// to finish (AwaitMode=Quorum) before shipping.
//
// Workflow shape:
//
//	┌─ branch-1: impl ──handoff──► review ─┐
//	│                                       │
//	├─ branch-2: impl ──handoff──► review ──┼──► synth (quorum=2)
//	│                                       │
//	└─ branch-3: impl ──handoff──► review ─┘
//
//	go run ./_examples/recipes/collab
package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Viking602/go-hydaelyn/orchestrator"
)

func main() {
	ctx := context.Background()
	rt := orchestrator.NewRuntime(orchestrator.Config{})

	branches := []string{"api", "ui", "data"}
	for _, b := range branches {
		rt.RegisterAgent(orchestrator.AgentProfile{ID: "impl-" + b, Role: "collab.implementer"})
		rt.RegisterAgent(orchestrator.AgentProfile{ID: "review-" + b, Role: "collab.reviewer"})
	}
	rt.RegisterAgent(orchestrator.AgentProfile{ID: "synth", Role: "collab.synthesizer"})

	run, _, err := rt.StartRun(ctx, orchestrator.StartRunCommand{Request: "ship feature X across 3 branches"})
	must(err)

	branchTaskIDs := make([]string, 0, len(branches))
	var wg sync.WaitGroup
	for _, b := range branches {
		taskID := "branch-" + b
		branchTaskIDs = append(branchTaskIDs, taskID)
		_, err := rt.CreateTask(ctx, orchestrator.CreateTaskCommand{
			RunID: run.ID, TaskID: taskID, OwnerAgentID: "impl-" + b,
		})
		must(err)
		wg.Add(1)
		go func(taskID, branch string) {
			defer wg.Done()
			runBranch(ctx, rt, run.ID, taskID, branch)
		}(taskID, b)
	}

	synth, err := rt.CreateTask(ctx, orchestrator.CreateTaskCommand{
		RunID: run.ID, TaskID: "synthesize", OwnerAgentID: "synth",
		DependsOn:          branchTaskIDs,
		AwaitMode:          orchestrator.AwaitModeQuorum,
		AwaitQuorum:        2,
		OnDependencyFailed: orchestrator.OnDependencyFailedContinue,
	})
	must(err)
	wg.Wait()
	runOnce(ctx, rt, run.ID, synth.ID, "synth", orchestrator.TypedReport{
		Status: orchestrator.ReportStatusSuccess, Summary: "feature X shipped",
	})
	fmt.Println("collab recipe complete (quorum=2 of 3 branches)")
}

func runBranch(ctx context.Context, rt *orchestrator.Runtime, runID, taskID, branch string) {
	implID, reviewID := "impl-"+branch, "review-"+branch
	task, err := rt.Task(ctx, runID, taskID)
	must(err)
	// Implementer hands off to reviewer.
	must(rt.RequestHandoff(ctx, orchestrator.HandoffCommand{
		RunID: runID, TaskID: taskID, TaskVersion: task.Version,
		FromAgentID: implID, ToAgentID: reviewID,
		HandoffContext: "draft ready for review",
	}))
	runOnce(ctx, rt, runID, taskID, reviewID, orchestrator.TypedReport{
		Status: orchestrator.ReportStatusSuccess, Summary: "branch " + branch + " reviewed",
	})
}

func runOnce(ctx context.Context, rt *orchestrator.Runtime, runID, taskID, agentID string, report orchestrator.TypedReport) {
	env, err := rt.DispatchTask(ctx, orchestrator.DispatchTaskCommand{RunID: runID, TaskID: taskID, TargetAgentID: agentID})
	must(err)
	lease, _, err := rt.AcquireTaskExecution(ctx, orchestrator.AcquireTaskExecutionCommand{
		RunID: runID, TaskID: taskID, EnvelopeID: env.ID,
		HolderType: orchestrator.HolderAgent, HolderID: agentID, TTL: time.Minute,
	})
	must(err)
	task, err := rt.Task(ctx, runID, taskID)
	must(err)
	must(rt.SubmitTypedReport(ctx, orchestrator.SubmitTypedReportCommand{
		RunID: runID, TaskID: taskID, LeaseID: lease.ID,
		HolderType: orchestrator.HolderAgent, HolderID: agentID,
		TaskVersion: task.Version, Report: report,
	}))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
