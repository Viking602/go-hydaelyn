// Recipe: research — N parallel researchers contribute evidence, a
// supervisor waits for the corpus, then synthesizes a finding.
//
// Workflow shape:
//
//	             ┌─ researcher-1 ─┐
//	supervisor ──┼─ researcher-2 ─┼──► WaitForBlackboard ──► synthesizer
//	             └─ researcher-N ─┘
//
//	go run ./_examples/recipes/research
package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Viking602/go-hydaelyn/orchestrator"
)

const (
	roleResearcher  = "research.researcher"
	roleSynthesizer = "research.synthesizer"
)

func main() {
	ctx := context.Background()
	rt := orchestrator.NewRuntime(orchestrator.Config{})

	researchers := []string{"alpha", "beta", "gamma"}
	for _, id := range researchers {
		rt.RegisterAgent(orchestrator.AgentProfile{ID: id, Role: roleResearcher})
	}
	rt.RegisterAgent(orchestrator.AgentProfile{ID: "synth", Role: roleSynthesizer})

	run, _, err := rt.StartRun(ctx, orchestrator.StartRunCommand{Request: "compare Go agent runtimes"})
	must(err)

	// Each researcher writes one Evidence item to the blackboard.
	var wg sync.WaitGroup
	for i, id := range researchers {
		taskID := "research-" + id
		_, err := rt.CreateTask(ctx, orchestrator.CreateTaskCommand{
			RunID: run.ID, TaskID: taskID, OwnerAgentID: id,
		})
		must(err)
		wg.Add(1)
		go func(taskID, agentID string, idx int) {
			defer wg.Done()
			runResearcher(ctx, rt, run.ID, taskID, agentID, idx)
		}(taskID, id, i)
	}

	// Synthesizer waits until all 3 evidence items land, then writes a Finding.
	want := len(researchers)
	items, err := rt.WaitForBlackboard(ctx, run.ID,
		orchestrator.BlackboardFilter{ItemTypes: []orchestrator.BlackboardItemType{orchestrator.BlackboardItemEvidence}},
		func(items []orchestrator.BlackboardItem) bool { return len(items) >= want },
		5*time.Second,
	)
	must(err)
	wg.Wait()

	must(rt.WriteItem(ctx, orchestrator.BlackboardItem{
		RunID:      run.ID,
		Type:       orchestrator.BlackboardItemFinding,
		Source:     orchestrator.SourceIdentity{Type: orchestrator.SourceAgent, ID: "synth"},
		Content:    fmt.Sprintf("synthesised %d evidence items into a recommendation", len(items)),
		Visibility: orchestrator.BlackboardVisibilityAgentVisible,
	}))
	fmt.Printf("research recipe complete: %d evidence → 1 finding\n", len(items))
}

func runResearcher(ctx context.Context, rt *orchestrator.Runtime, runID, taskID, agentID string, idx int) {
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
		Content:    fmt.Sprintf("evidence-%d from %s", idx+1, agentID),
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

func must(err error) {
	if err != nil {
		panic(err)
	}
}
