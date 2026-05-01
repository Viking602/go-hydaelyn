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

	hydaelyn "github.com/Viking602/go-hydaelyn"
)

func main() {
	ctx := context.Background()
	runner := hydaelyn.New()

	branches := []string{"api", "ui", "data"}
	for _, b := range branches {
		runner.RegisterAgent(hydaelyn.AgentProfile{ID: "impl-" + b, Role: "collab.implementer"})
		runner.RegisterAgent(hydaelyn.AgentProfile{ID: "review-" + b, Role: "collab.reviewer"})
	}
	runner.RegisterAgent(hydaelyn.AgentProfile{ID: "synth", Role: "collab.synthesizer"})

	run, _, err := runner.StartRun(ctx, hydaelyn.StartRunCommand{Request: "ship feature X across 3 branches"})
	must(err)

	branchTaskIDs := make([]string, 0, len(branches))
	var wg sync.WaitGroup
	for _, b := range branches {
		taskID := "branch-" + b
		branchTaskIDs = append(branchTaskIDs, taskID)
		_, err := runner.CreateTask(ctx, hydaelyn.CreateTaskCommand{
			RunID: run.ID, TaskID: taskID, OwnerAgentID: "impl-" + b,
		})
		must(err)
		wg.Add(1)
		go func(taskID, branch string) {
			defer wg.Done()
			runBranch(ctx, runner, run.ID, taskID, branch)
		}(taskID, b)
	}

	synth, err := runner.CreateTask(ctx, hydaelyn.CreateTaskCommand{
		RunID: run.ID, TaskID: "synthesize", OwnerAgentID: "synth",
		DependsOn:          branchTaskIDs,
		AwaitMode:          hydaelyn.AwaitModeQuorum,
		AwaitQuorum:        2,
		OnDependencyFailed: hydaelyn.OnDependencyFailedContinue,
	})
	must(err)
	wg.Wait()
	runOnce(ctx, runner, run.ID, synth.ID, "synth", hydaelyn.TypedReport{
		Status: hydaelyn.ReportStatusSuccess, Summary: "feature X shipped",
	})
	fmt.Println("collab recipe complete (quorum=2 of 3 branches)")
}

func runBranch(ctx context.Context, runner *hydaelyn.Runner, runID, taskID, branch string) {
	implID, reviewID := "impl-"+branch, "review-"+branch
	task, err := runner.Task(ctx, runID, taskID)
	must(err)
	// Implementer hands off to reviewer.
	must(runner.RequestHandoff(ctx, hydaelyn.HandoffCommand{
		RunID: runID, TaskID: taskID, TaskVersion: task.Version,
		FromAgentID: implID, ToAgentID: reviewID,
		HandoffContext: "draft ready for review",
	}))
	runOnce(ctx, runner, runID, taskID, reviewID, hydaelyn.TypedReport{
		Status: hydaelyn.ReportStatusSuccess, Summary: "branch " + branch + " reviewed",
	})
}

func runOnce(ctx context.Context, runner *hydaelyn.Runner, runID, taskID, agentID string, report hydaelyn.TypedReport) {
	env, err := runner.DispatchTask(ctx, hydaelyn.DispatchTaskCommand{RunID: runID, TaskID: taskID, TargetAgentID: agentID})
	must(err)
	lease, _, err := runner.AcquireTaskExecution(ctx, hydaelyn.AcquireTaskExecutionCommand{
		RunID: runID, TaskID: taskID, EnvelopeID: env.ID,
		HolderType: hydaelyn.HolderAgent, HolderID: agentID, TTL: time.Minute,
	})
	must(err)
	task, err := runner.Task(ctx, runID, taskID)
	must(err)
	must(runner.SubmitTypedReport(ctx, hydaelyn.SubmitTypedReportCommand{
		RunID: runID, TaskID: taskID, LeaseID: lease.ID,
		HolderType: hydaelyn.HolderAgent, HolderID: agentID,
		TaskVersion: task.Version, Report: report,
	}))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
