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

	"github.com/Viking602/go-hydaelyn"
	"github.com/Viking602/go-hydaelyn/api"
)

const (
	roleResearcher  = "research.researcher"
	roleSynthesizer = "research.synthesizer"
)

func main() {
	ctx := context.Background()
	runner := hydaelyn.NewDevelopment()

	researchers := []string{"alpha", "beta", "gamma"}
	for _, id := range researchers {
		runner.RegisterAgent(api.AgentProfile{ID: id, Role: roleResearcher})
	}
	runner.RegisterAgent(api.AgentProfile{ID: "synth", Role: roleSynthesizer})

	run, _, err := runner.StartRun(ctx, api.StartRunCommand{Request: "compare Go agent runtimes"})
	must(err)

	// Each researcher writes one Evidence item to the blackboard.
	var wg sync.WaitGroup
	for i, id := range researchers {
		taskID := "research-" + id
		_, err := runner.CreateTask(ctx, api.CreateTaskCommand{
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
		api.BlackboardFilter{ItemTypes: []api.BlackboardItemType{api.BlackboardItemEvidence}},
		func(items []api.BlackboardItem) bool { return len(items) >= want },
		5*time.Second,
	)
	must(err)
	wg.Wait()

	must(runner.WriteItem(ctx, api.BlackboardItem{
		RunID:      run.ID,
		Type:       api.BlackboardItemFinding,
		Source:     api.SourceIdentity{Type: api.SourceAgent, ID: "synth"},
		Content:    fmt.Sprintf("synthesized %d evidence items into a recommendation", len(items)),
		Visibility: api.BlackboardVisibilityAgentVisible,
	}))
	fmt.Printf("research recipe complete: %d evidence → 1 finding\n", len(items))
}

func runResearcher(ctx context.Context, runner *hydaelyn.Runner, runID, taskID, agentID string, idx int) {
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
		Content:    fmt.Sprintf("evidence-%d from %s", idx+1, agentID),
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

func must(err error) {
	if err != nil {
		panic(err)
	}
}
