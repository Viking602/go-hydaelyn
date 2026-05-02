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

	hydaelyn "github.com/Viking602/go-hydaelyn"
	"github.com/Viking602/go-hydaelyn/api"
)

func main() {
	ctx := context.Background()
	runner := hydaelyn.New()
	experts := []string{"security", "frontend", "platform"}
	for _, id := range experts {
		runner.RegisterAgent(api.AgentProfile{ID: id, Role: "panel.expert"})
	}
	runner.RegisterAgent(api.AgentProfile{ID: "synth", Role: "panel.synthesizer"})

	run, _, err := runner.StartRun(ctx, api.StartRunCommand{Request: "feature launch panel"})
	must(err)

	expertTaskIDs := make([]string, 0, len(experts))
	var wg sync.WaitGroup
	for _, id := range experts {
		taskID := "review-" + id
		expertTaskIDs = append(expertTaskIDs, taskID)
		_, err := runner.CreateTask(ctx, api.CreateTaskCommand{
			RunID: run.ID, TaskID: taskID, OwnerAgentID: id,
		})
		must(err)
		wg.Add(1)
		go func(taskID, agentID string) {
			defer wg.Done()
			runOnce(ctx, runner, run.ID, taskID, agentID, api.ReportStatusSuccess)
		}(taskID, id)
	}

	// Synthesizer needs only quorum (>= ceil(N/2)) of panel members to ship.
	synth, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID: run.ID, TaskID: "synthesize", OwnerAgentID: "synth",
		DependsOn:          expertTaskIDs,
		AwaitMode:          api.AwaitModeQuorum,
		AwaitQuorum:        2,
		OnDependencyFailed: api.OnDependencyFailedContinue,
	})
	must(err)
	wg.Wait()

	runOnce(ctx, runner, run.ID, synth.ID, "synth", api.ReportStatusSuccess)
	fmt.Println("quorum reached — synthesizer ran")
}

func runOnce(ctx context.Context, runner *hydaelyn.Runner, runID, taskID, agentID string, status api.ReportStatus) {
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
		TaskVersion: task.Version,
		Report:      api.TypedReport{Status: status, Summary: "panel review " + agentID},
	}))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
