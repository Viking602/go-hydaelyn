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

	"github.com/Viking602/go-hydaelyn/orchestrator"
)

func main() {
	ctx := context.Background()
	rt := orchestrator.NewRuntime(orchestrator.Config{})

	researchers := []string{"r1", "r2"}
	for _, id := range researchers {
		rt.RegisterAgent(orchestrator.AgentProfile{ID: id, Role: "deepsearch.researcher"})
	}
	rt.RegisterAgent(orchestrator.AgentProfile{ID: "verifier", Role: "deepsearch.verifier"})
	rt.RegisterAgent(orchestrator.AgentProfile{ID: "synth", Role: "deepsearch.synthesizer"})

	run, _, err := rt.StartRun(ctx, orchestrator.StartRunCommand{Request: "deep dive on rate-limiting"})
	must(err)

	// Stage 1: parallel research.
	var wg sync.WaitGroup
	researchTaskIDs := make([]string, 0, len(researchers))
	for i, id := range researchers {
		taskID := "research-" + id
		researchTaskIDs = append(researchTaskIDs, taskID)
		_, err := rt.CreateTask(ctx, orchestrator.CreateTaskCommand{RunID: run.ID, TaskID: taskID, OwnerAgentID: id})
		must(err)
		wg.Add(1)
		go func(taskID, agentID string, idx int) {
			defer wg.Done()
			runWithEvidence(ctx, rt, run.ID, taskID, agentID, idx)
		}(taskID, id, i)
	}

	// Stage 2: verifier blocks on all researchers.
	verifyTask, err := rt.CreateTask(ctx, orchestrator.CreateTaskCommand{
		RunID: run.ID, TaskID: "verify", OwnerAgentID: "verifier",
		DependsOn: researchTaskIDs, AwaitMode: orchestrator.AwaitModeAll,
	})
	must(err)
	wg.Wait()
	runOnce(ctx, rt, run.ID, verifyTask.ID, "verifier", orchestrator.TypedReport{
		Status: orchestrator.ReportStatusSuccess, Summary: "evidence is consistent",
	})

	// Stage 3: synthesizer publishes the finding.
	synth, err := rt.CreateTask(ctx, orchestrator.CreateTaskCommand{
		RunID: run.ID, TaskID: "synthesize", OwnerAgentID: "synth",
		DependsOn: []string{verifyTask.ID}, AwaitMode: orchestrator.AwaitModeAll,
	})
	must(err)
	runOnce(ctx, rt, run.ID, synth.ID, "synth", orchestrator.TypedReport{
		Status: orchestrator.ReportStatusSuccess, Summary: "rate-limiter recommendation: token bucket",
	})

	timeline, _ := rt.RunTimeline(ctx, run.ID)
	fmt.Printf("deepsearch recipe complete: %d timeline items\n", len(timeline))
}

func runWithEvidence(ctx context.Context, rt *orchestrator.Runtime, runID, taskID, agentID string, idx int) {
	env, err := rt.DispatchTask(ctx, orchestrator.DispatchTaskCommand{RunID: runID, TaskID: taskID, TargetAgentID: agentID})
	must(err)
	lease, _, err := rt.AcquireTaskExecution(ctx, orchestrator.AcquireTaskExecutionCommand{
		RunID: runID, TaskID: taskID, EnvelopeID: env.ID,
		HolderType: orchestrator.HolderAgent, HolderID: agentID, TTL: time.Minute,
	})
	must(err)
	must(rt.WriteItem(ctx, orchestrator.BlackboardItem{
		RunID: runID, TaskID: taskID,
		Type:       orchestrator.BlackboardItemEvidence,
		Source:     orchestrator.SourceIdentity{Type: orchestrator.SourceAgent, ID: agentID},
		Content:    fmt.Sprintf("snippet-%d from %s", idx+1, agentID),
		Visibility: orchestrator.BlackboardVisibilityAgentVisible,
	}))
	task, err := rt.Task(ctx, runID, taskID)
	must(err)
	must(rt.SubmitTypedReport(ctx, orchestrator.SubmitTypedReportCommand{
		RunID: runID, TaskID: taskID, LeaseID: lease.ID,
		HolderType: orchestrator.HolderAgent, HolderID: agentID,
		TaskVersion: task.Version,
		Report:      orchestrator.TypedReport{Status: orchestrator.ReportStatusSuccess, Summary: "evidence shipped"},
	}))
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
