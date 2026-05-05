// evaluation walks a finished run's event stream and computes simple metrics.
// The framework only emits Events — evaluators are entirely user-defined.
//
//	go run ./_examples/evaluation
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
	runner := hydaelyn.New()
	for _, id := range []string{"a", "b"} {
		runner.RegisterAgent(api.AgentProfile{ID: id})
	}

	run, _, err := runner.StartRun(ctx, api.StartRunCommand{Request: "two parallel tasks"})
	must(err)
	for _, id := range []string{"a", "b"} {
		taskID := "t-" + id
		_, err := runner.CreateTask(ctx, api.CreateTaskCommand{RunID: run.ID, TaskID: taskID, OwnerAgentID: id})
		must(err)
		runOnce(ctx, runner, run.ID, taskID, id)
	}

	events, err := runner.RunEvents(ctx, run.ID)
	must(err)
	report := evaluate(events)
	out, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(out))
}

type metrics struct {
	TotalEvents  int            `json:"totalEvents"`
	ByEventKind  map[string]int `json:"byEventKind"`
	TaskCount    int            `json:"taskCount"`
	SuccessCount int            `json:"successCount"`
}

func evaluate(events []api.Event) metrics {
	m := metrics{ByEventKind: map[string]int{}}
	m.TotalEvents = len(events)
	for _, ev := range events {
		m.ByEventKind[string(ev.Type)]++
		switch ev.Type {
		case api.EventTaskCreated:
			m.TaskCount++
		case api.EventTaskCompleted:
			m.SuccessCount++
		}
	}
	return m
}

func runOnce(ctx context.Context, runner *hydaelyn.Runner, runID, taskID, agentID string) {
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
		Report:      api.TypedReport{Status: api.ReportStatusSuccess, Summary: "ok"},
	}))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
