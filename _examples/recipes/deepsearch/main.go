// Recipe: deepsearch — research + verification stage gated by AwaitMode=All.
// The verifier only runs after every researcher has shipped, then a final
// synthesizer reads both evidence and verification before writing a Finding.
//
// Workflow shape:
//
//	researchers ──► verifier (AwaitMode=All) ──► synthesizer
//	     │                                              ▲
//	     └────────► blackboard (Evidence) ──────────────┘
//
//	go run ./_examples/recipes/deepsearch
package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Viking602/go-hydaelyn"
	"github.com/Viking602/go-hydaelyn/api"
)

func main() {
	ctx := context.Background()
	runner := hydaelyn.New()

	researchers := []string{"r1", "r2"}
	for _, id := range researchers {
		runner.RegisterAgent(api.AgentProfile{ID: id, Role: "deepsearch.researcher"})
	}
	runner.RegisterAgent(api.AgentProfile{ID: "verifier", Role: "deepsearch.verifier"})
	runner.RegisterAgent(api.AgentProfile{ID: "synth", Role: "deepsearch.synthesizer"})

	run, _, err := runner.StartRun(ctx, api.StartRunCommand{Request: "deep dive on rate-limiting"})
	must(err)

	// Stage 1: parallel research.
	var wg sync.WaitGroup
	researchTaskIDs := make([]string, 0, len(researchers))
	for i, id := range researchers {
		taskID := "research-" + id
		researchTaskIDs = append(researchTaskIDs, taskID)
		_, err := runner.CreateTask(ctx, api.CreateTaskCommand{RunID: run.ID, TaskID: taskID, OwnerAgentID: id})
		must(err)
		wg.Add(1)
		go func(taskID, agentID string, idx int) {
			defer wg.Done()
			runWithEvidence(ctx, runner, run.ID, taskID, agentID, idx)
		}(taskID, id, i)
	}

	// Stage 2: verifier blocks on all researchers.
	verifyTask, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID: run.ID, TaskID: "verify", OwnerAgentID: "verifier",
		DependsOn: researchTaskIDs, AwaitMode: api.AwaitModeAll,
	})
	must(err)
	wg.Wait()
	runOnce(ctx, runner, run.ID, verifyTask.ID, "verifier", api.TypedReport{
		Status: api.ReportStatusSuccess, Summary: "evidence is consistent",
	})

	// Stage 3: synthesizer publishes the finding.
	synth, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID: run.ID, TaskID: "synthesize", OwnerAgentID: "synth",
		DependsOn: []string{verifyTask.ID}, AwaitMode: api.AwaitModeAll,
	})
	must(err)
	runOnce(ctx, runner, run.ID, synth.ID, "synth", api.TypedReport{
		Status: api.ReportStatusSuccess, Summary: "rate-limiter recommendation: token bucket",
	})

	timeline, _ := runner.RunTimeline(ctx, run.ID)
	fmt.Printf("deepsearch recipe complete: %d timeline items\n", len(timeline))
}

func runWithEvidence(ctx context.Context, runner *hydaelyn.Runner, runID, taskID, agentID string, idx int) {
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{RunID: runID, TaskID: taskID, TargetAgentID: agentID})
	must(err)
	lease, _, err := runner.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{
		RunID: runID, TaskID: taskID, EnvelopeID: env.ID,
		HolderType: api.HolderAgent, HolderID: agentID, TTL: time.Minute,
	})
	must(err)
	must(runner.WriteItem(ctx, api.BlackboardItem{
		RunID: runID, TaskID: taskID,
		Type:       api.BlackboardItemEvidence,
		Source:     api.SourceIdentity{Type: api.SourceAgent, ID: agentID},
		Content:    fmt.Sprintf("snippet-%d from %s", idx+1, agentID),
		Visibility: api.BlackboardVisibilityAgentVisible,
	}))
	task, err := runner.Task(ctx, runID, taskID)
	must(err)
	must(runner.SubmitTypedReport(ctx, api.SubmitTypedReportCommand{
		RunID: runID, TaskID: taskID, LeaseID: lease.ID,
		HolderType: api.HolderAgent, HolderID: agentID,
		TaskVersion: task.Version,
		Report:      api.TypedReport{Status: api.ReportStatusSuccess, Summary: "evidence shipped"},
	}))
}

func runOnce(ctx context.Context, runner *hydaelyn.Runner, runID, taskID, agentID string, report api.TypedReport) {
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{RunID: runID, TaskID: taskID, TargetAgentID: agentID})
	must(err)
	lease, _, err := runner.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{
		RunID: runID, TaskID: taskID, EnvelopeID: env.ID,
		HolderType: api.HolderAgent, HolderID: agentID, TTL: time.Minute,
	})
	must(err)
	task, err := runner.Task(ctx, runID, taskID)
	must(err)
	must(runner.SubmitTypedReport(ctx, api.SubmitTypedReportCommand{
		RunID: runID, TaskID: taskID, LeaseID: lease.ID,
		HolderType: api.HolderAgent, HolderID: agentID,
		TaskVersion: task.Version, Report: report,
	}))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
