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

	"github.com/Viking602/go-hydaelyn/orchestrator"
)

func main() {
	ctx := context.Background()
	rt := orchestrator.NewRuntime(orchestrator.Config{})
	for _, id := range []string{"a", "b"} {
		rt.RegisterAgent(orchestrator.AgentProfile{ID: id})
	}

	run, _, err := rt.StartRun(ctx, orchestrator.StartRunCommand{Request: "two parallel tasks"})
	must(err)
	for _, id := range []string{"a", "b"} {
		taskID := "t-" + id
		_, err := rt.CreateTask(ctx, orchestrator.CreateTaskCommand{RunID: run.ID, TaskID: taskID, OwnerAgentID: id})
		must(err)
		runOnce(ctx, rt, run.ID, taskID, id)
	}

	events, err := rt.RunEvents(ctx, run.ID)
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

func evaluate(events []orchestrator.Event) metrics {
	m := metrics{ByEventKind: map[string]int{}}
	m.TotalEvents = len(events)
	for _, ev := range events {
		m.ByEventKind[string(ev.Type)]++
		switch ev.Type {
		case orchestrator.EventTaskCreated:
			m.TaskCount++
		case orchestrator.EventTaskCompleted:
			m.SuccessCount++
		}
	}
	return m
}

func runOnce(ctx context.Context, rt *orchestrator.Runtime, runID, taskID, agentID string) {
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
		Report:      orchestrator.TypedReport{Status: orchestrator.ReportStatusSuccess, Summary: "ok"},
	}))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
