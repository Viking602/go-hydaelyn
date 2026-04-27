// panel demonstrates AwaitMode=Quorum: a synthesizer task unblocks once
// 2 of 3 panel experts succeed. The framework gates dependency completion;
// the panel composition is user-defined.
//
//	go run ./_examples/panel
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
	experts := []string{"security", "frontend", "platform"}
	for _, id := range experts {
		rt.RegisterAgent(orchestrator.AgentProfile{ID: id, Role: "panel.expert"})
	}
	rt.RegisterAgent(orchestrator.AgentProfile{ID: "synth", Role: "panel.synthesizer"})

	run, _, err := rt.StartRun(ctx, orchestrator.StartRunCommand{Request: "feature launch panel"})
	must(err)

	expertTaskIDs := make([]string, 0, len(experts))
	var wg sync.WaitGroup
	for _, id := range experts {
		taskID := "review-" + id
		expertTaskIDs = append(expertTaskIDs, taskID)
		_, err := rt.CreateTask(ctx, orchestrator.CreateTaskCommand{
			RunID: run.ID, TaskID: taskID, OwnerAgentID: id,
		})
		must(err)
		wg.Add(1)
		go func(taskID, agentID string) {
			defer wg.Done()
			runOnce(ctx, rt, run.ID, taskID, agentID, orchestrator.ReportStatusSuccess)
		}(taskID, id)
	}

	// Synthesizer needs only quorum (>= ceil(N/2)) of panel members to ship.
	synth, err := rt.CreateTask(ctx, orchestrator.CreateTaskCommand{
		RunID: run.ID, TaskID: "synthesize", OwnerAgentID: "synth",
		DependsOn:          expertTaskIDs,
		AwaitMode:          orchestrator.AwaitModeQuorum,
		AwaitQuorum:        2,
		OnDependencyFailed: orchestrator.OnDependencyFailedContinue,
	})
	must(err)
	wg.Wait()

	runOnce(ctx, rt, run.ID, synth.ID, "synth", orchestrator.ReportStatusSuccess)
	fmt.Println("quorum reached — synthesizer ran")
}

func runOnce(ctx context.Context, rt *orchestrator.Runtime, runID, taskID, agentID string, status orchestrator.ReportStatus) {
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
		TaskVersion: task.Version,
		Report:      orchestrator.TypedReport{Status: status, Summary: "panel review " + agentID},
	}))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
