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

	"github.com/Viking602/go-hydaelyn/orchestrator"
)

const roleExpert = "panel.expert"

func main() {
	ctx := context.Background()
	rt := orchestrator.NewRuntime(orchestrator.Config{})

	experts := []string{"security", "frontend", "platform"}
	for _, id := range experts {
		rt.RegisterAgent(orchestrator.AgentProfile{ID: id, Role: roleExpert, Groups: []string{"panel"}})
	}
	rt.RegisterAgent(orchestrator.AgentProfile{ID: "synth", Role: "panel.synthesizer"})

	run, _, err := rt.StartRun(ctx, orchestrator.StartRunCommand{Request: "review feature launch"})
	must(err)

	// Heads-up fan-out by Role: one envelope per expert.
	heads, err := rt.CreateTask(ctx, orchestrator.CreateTaskCommand{
		RunID: run.ID, TaskID: "panel-notify", OwnerComponent: "orchestrator",
	})
	must(err)
	envs, err := rt.DispatchTaskFanOut(ctx, orchestrator.FanOutDispatchTaskCommand{
		RunID: run.ID, TaskID: heads.ID,
		To:      orchestrator.Address{Kind: orchestrator.AddressKindRole, Role: roleExpert},
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
		_, err := rt.CreateTask(ctx, orchestrator.CreateTaskCommand{
			RunID: run.ID, TaskID: taskID, OwnerAgentID: id,
		})
		must(err)
		wg.Add(1)
		go func(taskID, agentID string) {
			defer wg.Done()
			runExpert(ctx, rt, run.ID, taskID, agentID)
		}(taskID, id)
	}

	want := len(experts)
	claims, err := rt.WaitForBlackboard(ctx, run.ID,
		orchestrator.BlackboardFilter{ItemTypes: []orchestrator.BlackboardItemType{orchestrator.BlackboardItemClaim}},
		func(items []orchestrator.BlackboardItem) bool { return len(items) >= want },
		5*time.Second,
	)
	must(err)
	wg.Wait()

	synth, err := rt.CreateTask(ctx, orchestrator.CreateTaskCommand{
		RunID: run.ID, TaskID: "synthesize", OwnerAgentID: "synth",
		DependsOn: reviewIDs, AwaitMode: orchestrator.AwaitModeAll,
	})
	must(err)
	runOnce(ctx, rt, run.ID, synth.ID, "synth", orchestrator.TypedReport{
		Status:  orchestrator.ReportStatusSuccess,
		Summary: fmt.Sprintf("synthesised %d expert claims", len(claims)),
	})
	fmt.Printf("panel recipe complete: %d claims → 1 synthesis\n", len(claims))
}

func runExpert(ctx context.Context, rt *orchestrator.Runtime, runID, taskID, agentID string) {
	env, err := rt.DispatchTask(ctx, orchestrator.DispatchTaskCommand{RunID: runID, TaskID: taskID, TargetAgentID: agentID})
	must(err)
	lease, _, err := rt.AcquireTaskExecution(ctx, orchestrator.AcquireTaskExecutionCommand{
		RunID: runID, TaskID: taskID, EnvelopeID: env.ID,
		HolderType: orchestrator.HolderAgent, HolderID: agentID, TTL: time.Minute,
	})
	must(err)
	must(rt.WriteItem(ctx, orchestrator.BlackboardItem{
		RunID: runID, TaskID: taskID,
		Type:       orchestrator.BlackboardItemClaim,
		Source:     orchestrator.SourceIdentity{Type: orchestrator.SourceAgent, ID: agentID},
		Content:    "claim from expert " + agentID,
		Visibility: orchestrator.BlackboardVisibilityAgentVisible,
	}))
	task, err := rt.Task(ctx, runID, taskID)
	must(err)
	must(rt.SubmitTypedReport(ctx, orchestrator.SubmitTypedReportCommand{
		RunID: runID, TaskID: taskID, LeaseID: lease.ID,
		HolderType: orchestrator.HolderAgent, HolderID: agentID,
		TaskVersion: task.Version,
		Report:      orchestrator.TypedReport{Status: orchestrator.ReportStatusSuccess, Summary: "review " + agentID},
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
