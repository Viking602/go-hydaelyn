// Recipe: panel — domain experts each tagged by Role. The supervisor fans
// out one notification by AddressKindRole, then dispatches one review task
// per expert, each writing a Claim. Synthesizer waits for the corpus.
//
// Workflow shape:
//
//	supervisor ──fan-out (Role)──► [security, frontend, platform]
//	                                       │
//	                         each writes Claim → blackboard
//	                                       │
//	                                       ▼
//	                                 synthesizer
//
//	go run ./_examples/recipes/panel
package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	hydaelyn "github.com/Viking602/go-hydaelyn"
	"github.com/Viking602/go-hydaelyn/api"
)

const roleExpert = "panel.expert"

func main() {
	ctx := context.Background()
	runner := hydaelyn.New()

	experts := []string{"security", "frontend", "platform"}
	for _, id := range experts {
		runner.RegisterAgent(api.AgentProfile{ID: id, Role: roleExpert, Groups: []string{"panel"}})
	}
	runner.RegisterAgent(api.AgentProfile{ID: "synth", Role: "panel.synthesizer"})

	run, _, err := runner.StartRun(ctx, api.StartRunCommand{Request: "review feature launch"})
	must(err)

	// Heads-up fan-out by Role: one envelope per expert.
	heads, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID: run.ID, TaskID: "panel-notify", OwnerComponent: "orchestrator",
	})
	must(err)
	envs, err := runner.DispatchTaskFanOut(ctx, api.FanOutDispatchTaskCommand{
		RunID: run.ID, TaskID: heads.ID,
		To:      api.Address{Kind: api.AddressKindRole, Role: roleExpert},
		Payload: map[string]any{"alert": "panel review starting"},
	})
	must(err)
	fmt.Printf("notified %d experts via Role address\n", len(envs))

	// One review task per expert.
	var wg sync.WaitGroup
	reviewIDs := make([]string, 0, len(experts))
	for _, id := range experts {
		taskID := "review-" + id
		reviewIDs = append(reviewIDs, taskID)
		_, err := runner.CreateTask(ctx, api.CreateTaskCommand{
			RunID: run.ID, TaskID: taskID, OwnerAgentID: id,
		})
		must(err)
		wg.Add(1)
		go func(taskID, agentID string) {
			defer wg.Done()
			runExpert(ctx, runner, run.ID, taskID, agentID)
		}(taskID, id)
	}

	want := len(experts)
	claims, err := runner.WaitForBlackboard(ctx, run.ID,
		api.BlackboardFilter{ItemTypes: []api.BlackboardItemType{api.BlackboardItemClaim}},
		func(items []api.BlackboardItem) bool { return len(items) >= want },
		5*time.Second,
	)
	must(err)
	wg.Wait()

	synth, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID: run.ID, TaskID: "synthesize", OwnerAgentID: "synth",
		DependsOn: reviewIDs, AwaitMode: api.AwaitModeAll,
	})
	must(err)
	runOnce(ctx, runner, run.ID, synth.ID, "synth", api.TypedReport{
		Status:  api.ReportStatusSuccess,
		Summary: fmt.Sprintf("synthesised %d expert claims", len(claims)),
	})
	fmt.Printf("panel recipe complete: %d claims → 1 synthesis\n", len(claims))
}

func runExpert(ctx context.Context, runner *hydaelyn.Runner, runID, taskID, agentID string) {
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{RunID: runID, TaskID: taskID, TargetAgentID: agentID})
	must(err)
	lease, _, err := runner.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{
		RunID: runID, TaskID: taskID, EnvelopeID: env.ID,
		HolderType: api.HolderAgent, HolderID: agentID, TTL: time.Minute,
	})
	must(err)
	must(runner.WriteItem(ctx, api.BlackboardItem{
		RunID: runID, TaskID: taskID,
		Type:       api.BlackboardItemClaim,
		Source:     api.SourceIdentity{Type: api.SourceAgent, ID: agentID},
		Content:    "claim from expert " + agentID,
		Visibility: api.BlackboardVisibilityAgentVisible,
	}))
	task, err := runner.Task(ctx, runID, taskID)
	must(err)
	must(runner.SubmitTypedReport(ctx, api.SubmitTypedReportCommand{
		RunID: runID, TaskID: taskID, LeaseID: lease.ID,
		HolderType: api.HolderAgent, HolderID: agentID,
		TaskVersion: task.Version,
		Report:      api.TypedReport{Status: api.ReportStatusSuccess, Summary: "review " + agentID},
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
