package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Viking602/go-hydaelyn"
	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/multiagent"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/provider/scripted"
)

func TestTeamRunnerPersistsAndResumesSchedulerState(t *testing.T) {
	ctx := context.Background()
	runner := hydaelyn.NewDevelopment()
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{
		RunID:      "run-team",
		RootTaskID: "root",
		Request:    "run a team",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	researcher := multiagent.AgentClass{Name: "researcher", Instructions: "research", Model: "scripted"}
	writer := multiagent.AgentClass{Name: "writer", Instructions: "write", Model: "scripted"}
	team := multiagent.NewTeam("team").
		AddRole(researcher).
		AddRole(writer).
		WithScheduler(multiagent.SequentialScheduler{Classes: []multiagent.AgentClass{researcher, writer}})
	driver := scripted.New([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "done"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	})
	teamRunner := TeamRunner{
		Runner:    runner,
		Team:      *team,
		BuildDeps: agent.BuildDeps{Providers: provider.Single(driver)},
	}

	result, err := teamRunner.Start(ctx, run.ID)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if result.Ticks != 2 || len(result.State.Instances) != 2 {
		t.Fatalf("Start() result = %#v, want two durable ticks", result)
	}
	uow, err := runner.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	record, err := uow.TeamStates().LoadTeamState(ctx, run.ID)
	if err != nil {
		t.Fatalf("LoadTeamState() error = %v", err)
	}
	instances, err := uow.AgentInstances().ListAgentInstances(ctx, api.AgentInstanceSelector{RunID: run.ID})
	if err != nil {
		t.Fatalf("ListAgentInstances() error = %v", err)
	}
	if err := uow.Rollback(ctx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if record.Tick != 2 || len(instances) != 2 {
		t.Fatalf("durable team state tick=%d instances=%d, want 2 and 2", record.Tick, len(instances))
	}
	var checkpoint multiagent.TeamState
	if err := json.Unmarshal(record.State, &checkpoint); err != nil {
		t.Fatalf("Unmarshal(checkpoint) error = %v", err)
	}
	if checkpoint.Tick != 2 || len(checkpoint.Tasks) != 2 {
		t.Fatalf("checkpoint = %#v, want two completed tasks", checkpoint)
	}
	if len(checkpoint.Tasks[1].Input) == 0 {
		t.Fatalf("checkpoint dropped the previous report input: %#v", checkpoint.Tasks[1])
	}
	completedRun, err := runner.Run(ctx, run.ID)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if completedRun.Status != api.RunStatusCompleted {
		t.Fatalf("durable run status = %q, want completed", completedRun.Status)
	}

	resumed, err := teamRunner.Resume(ctx, run.ID)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if resumed.Ticks != 2 || len(resumed.State.Instances) != 2 {
		t.Fatalf("Resume() result = %#v, want unchanged terminal snapshot", resumed)
	}
}

func TestRunnerExecutorPersistsTypedHandoff(t *testing.T) {
	ctx := context.Background()
	runner := hydaelyn.NewDevelopment()
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{
		RunID:      "run-handoff",
		RootTaskID: "root",
		Request:    "handoff",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	class := multiagent.AgentClass{
		Name:         "receiver",
		Instructions: "receive",
		Model:        "scripted",
		InputSchema:  json.RawMessage(`{"type":"object","required":["value"],"properties":{"value":{"type":"string"}}}`),
	}
	driver := scripted.New([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "accepted"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	})
	dispatch := multiagent.Dispatch{
		To:        "instance-1",
		ClassName: class.Name,
		Task: api.Task{
			ID:          "task-1",
			RunID:       run.ID,
			Type:        api.TaskTypeWorker,
			Goal:        class.Instructions,
			InputSchema: class.InputSchema,
		},
		Input: json.RawMessage(`{"value":"evidence"}`),
		Handoff: &multiagent.Handoff{
			From:    "instance-0",
			Payload: json.RawMessage(`{"value":"evidence"}`),
		},
	}
	executor := RunnerExecutor{
		Runner:    runner,
		Classes:   map[string]multiagent.AgentClass{class.Name: class},
		BuildDeps: agent.BuildDeps{Providers: provider.Single(driver)},
	}
	report, err := executor.Execute(ctx, dispatch)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if report.Status != api.ReportStatusSuccess {
		t.Fatalf("Execute() report = %#v, want success", report)
	}
	uow, err := runner.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	handoffs, err := uow.Handoffs().ListHandoffs(ctx, api.HandoffSelector{RunID: run.ID})
	if err != nil {
		t.Fatalf("ListHandoffs() error = %v", err)
	}
	if err := uow.Rollback(ctx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if len(handoffs) != 1 || handoffs[0].From != "instance-0" || handoffs[0].To != dispatch.To {
		t.Fatalf("persisted handoffs = %#v, want one typed handoff", handoffs)
	}
}

func TestTeamRunnerRejectsConcurrentResume(t *testing.T) {
	ctx := context.Background()
	runner := hydaelyn.NewDevelopment()
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{
		RunID:      "run-team-owner",
		RootTaskID: "root",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	teamRunner := TeamRunner{
		Runner: runner,
		Team: multiagent.Team{Scheduler: multiagent.SchedulerFunc(func(context.Context, multiagent.TeamState) ([]multiagent.Dispatch, error) {
			close(entered)
			<-release
			return nil, nil
		})},
	}
	done := make(chan error, 1)
	go func() {
		_, err := teamRunner.Start(ctx, run.ID)
		done <- err
	}()
	<-entered
	if _, err := teamRunner.Resume(ctx, run.ID); !errors.Is(err, api.ErrLeaseNotActive) {
		t.Fatalf("concurrent Resume() error = %v, want ErrLeaseNotActive", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}
