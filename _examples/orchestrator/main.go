package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Viking602/go-hydaelyn"
	"github.com/Viking602/go-hydaelyn/api"
)

func main() {
	ctx := context.Background()
	runner := hydaelyn.NewDevelopment()

	run, err := runner.QueueRun(ctx, api.StartRunCommand{
		Request: "prepare a launch checklist",
	})
	if err != nil {
		panic(err)
	}

	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "research-1",
		OwnerAgentID: "agent-a",
	})
	if err != nil {
		panic(err)
	}
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
		RunID:         run.ID,
		TaskID:        task.ID,
		TargetAgentID: "agent-a",
	})
	if err != nil {
		panic(err)
	}
	lease, _, err := runner.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{
		RunID:      run.ID,
		TaskID:     task.ID,
		EnvelopeID: env.ID,
		HolderType: api.HolderAgent,
		HolderID:   "agent-a",
		TTL:        time.Minute,
	})
	if err != nil {
		panic(err)
	}
	if err := runner.SubmitTypedReport(ctx, api.SubmitTypedReportCommand{
		RunID:       run.ID,
		TaskID:      task.ID,
		LeaseID:     lease.ID,
		HolderType:  api.HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: task.Version,
		Report:      api.TypedReport{Status: api.ReportStatusSuccess, Summary: "launch checklist ready"},
	}); err != nil {
		panic(err)
	}

	projection, err := runner.ReplayRunStateContext(ctx, run.ID)
	if err != nil {
		panic(err)
	}
	payload, _ := json.MarshalIndent(projection, "", "  ")
	fmt.Println(string(payload))
}
