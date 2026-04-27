package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Viking602/go-hydaelyn"
)

func main() {
	ctx := context.Background()
	runner := hydaelyn.New(hydaelyn.Config{})

	run, err := runner.QueueRun(ctx, hydaelyn.StartRunCommand{
		Request: "prepare a launch checklist",
	})
	if err != nil {
		panic(err)
	}

	task, err := runner.CreateTask(ctx, hydaelyn.CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "research-1",
		OwnerAgentID: "agent-a",
	})
	if err != nil {
		panic(err)
	}
	env, err := runner.DispatchTask(ctx, hydaelyn.DispatchTaskCommand{
		RunID:         run.ID,
		TaskID:        task.ID,
		TargetAgentID: "agent-a",
	})
	if err != nil {
		panic(err)
	}
	lease, _, err := runner.AcquireTaskExecution(ctx, hydaelyn.AcquireTaskExecutionCommand{
		RunID:      run.ID,
		TaskID:     task.ID,
		EnvelopeID: env.ID,
		HolderType: hydaelyn.HolderAgent,
		HolderID:   "agent-a",
		TTL:        time.Minute,
	})
	if err != nil {
		panic(err)
	}
	if err := runner.SubmitTypedReport(ctx, hydaelyn.SubmitTypedReportCommand{
		RunID:       run.ID,
		TaskID:      task.ID,
		LeaseID:     lease.ID,
		HolderType:  hydaelyn.HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: task.Version,
		Report:      hydaelyn.TypedReport{Status: hydaelyn.ReportStatusSuccess, Summary: "launch checklist ready"},
	}); err != nil {
		panic(err)
	}

	projection, err := runner.ReplayRunState(run.ID)
	if err != nil {
		panic(err)
	}
	payload, _ := json.MarshalIndent(projection, "", "  ")
	fmt.Println(string(payload))
}
