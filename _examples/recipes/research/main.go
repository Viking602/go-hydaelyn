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

	hydaelyn "github.com/Viking602/go-hydaelyn"
)

const (
	roleResearcher  = "research.researcher"
	roleSynthesizer = "research.synthesizer"
)

func main() {
	ctx := context.Background()
	runner := hydaelyn.New()

	researchers := []string{"alpha", "beta", "gamma"}
	for _, id := range researchers {
		runner.RegisterAgent(hydaelyn.AgentProfile{ID: id, Role: roleResearcher})
	}
	runner.RegisterAgent(hydaelyn.AgentProfile{ID: "synth", Role: roleSynthesizer})

	run, _, err := runner.StartRun(ctx, hydaelyn.StartRunCommand{Request: "compare Go agent runtimes"})
	must(err)

	// Each researcher writes one Evidence item to the blackboard.
	var wg sync.WaitGroup
	for i, id := range researchers {
		taskID := "research-" + id
		_, err := runner.CreateTask(ctx, hydaelyn.CreateTaskCommand{
			RunID: run.ID, TaskID: taskID, OwnerAgentID: id,
		})
		must(err)
		wg.Add(1)
		go func(taskID, agentID string, idx int) {
			defer wg.Done()
			runResearcher(ctx, runner, run.ID, taskID, agentID, idx)
		}(taskID, id, i)
	}

	// Synthesizer waits until all 3 evidence items land, then writes a Finding.
	want := len(researchers)
	items, err := runner.WaitForBlackboard(ctx, run.ID,
		hydaelyn.BlackboardFilter{ItemTypes: []hydaelyn.BlackboardItemType{hydaelyn.BlackboardItemEvidence}},
		func(items []hydaelyn.BlackboardItem) bool { return len(items) >= want },
		5*time.Second,
	)
	must(err)
	wg.Wait()

	must(runner.WriteItem(ctx, hydaelyn.BlackboardItem{
		RunID:      run.ID,
		Type:       hydaelyn.BlackboardItemFinding,
		Source:     hydaelyn.SourceIdentity{Type: hydaelyn.SourceAgent, ID: "synth"},
		Content:    fmt.Sprintf("synthesised %d evidence items into a recommendation", len(items)),
		Visibility: hydaelyn.BlackboardVisibilityAgentVisible,
	}))
	fmt.Printf("research recipe complete: %d evidence → 1 finding\n", len(items))
}

func runResearcher(ctx context.Context, runner *hydaelyn.Runner, runID, taskID, agentID string, idx int) {
	env, err := runner.DispatchTask(ctx, hydaelyn.DispatchTaskCommand{RunID: runID, TaskID: taskID, TargetAgentID: agentID})
	must(err)
	lease, _, err := runner.AcquireTaskExecution(ctx, hydaelyn.AcquireTaskExecutionCommand{
		RunID: runID, TaskID: taskID, EnvelopeID: env.ID,
		HolderType: hydaelyn.HolderAgent, HolderID: agentID, TTL: time.Minute,
	})
	must(err)
	must(runner.WriteItem(ctx, hydaelyn.BlackboardItem{
		RunID: runID, TaskID: taskID,
		Type:       hydaelyn.BlackboardItemEvidence,
		Source:     hydaelyn.SourceIdentity{Type: hydaelyn.SourceAgent, ID: agentID},
		Content:    fmt.Sprintf("evidence-%d from %s", idx+1, agentID),
		Visibility: hydaelyn.BlackboardVisibilityAgentVisible,
	}))
	task, err := runner.Task(ctx, runID, taskID)
	must(err)
	must(runner.SubmitTypedReport(ctx, hydaelyn.SubmitTypedReportCommand{
		RunID: runID, TaskID: taskID, LeaseID: lease.ID,
		HolderType: hydaelyn.HolderAgent, HolderID: agentID,
		TaskVersion: task.Version,
		Report:      hydaelyn.TypedReport{Status: hydaelyn.ReportStatusSuccess, Summary: "evidence shipped"},
	}))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
