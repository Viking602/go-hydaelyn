package mailbox_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/mailbox"
	"github.com/Viking602/venat/internal/memory"
	runsvc "github.com/Viking602/venat/internal/run"
)

func mailboxIDGenerator() mailbox.IDGenerator {
	next := 0
	return func(prefix string) string {
		next++
		return fmt.Sprintf("%s-%d", prefix, next)
	}
}

func TestDispatchCopiesTaskDataflowIntoEnvelope(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	run, _, err := runsvc.Start(ctx, uow, func(prefix string) string { return prefix + "-seed" }, runsvc.StartInput{
		RunID:      "run-1",
		RootTaskID: "root",
		Request:    "dispatch",
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	task := model.Task{
		ID:             "task-1",
		RunID:          run.ID,
		Status:         model.TaskStatusRouted,
		Version:        4,
		ReadSelectors:  []model.BlackboardSelector{{Keys: []string{"brief"}}},
		WriteTargets:   []string{"findings"},
		RetryPolicy:    model.RetryPolicy{MaxAttempts: 2},
		OwnerAgentID:   "agent-1",
		OwnerComponent: "worker",
	}
	if err := uow.Tasks().SaveTask(ctx, task); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}
	payload := map[string]any{"goal": "collect"}
	env, err := mailbox.Dispatch(ctx, uow, mailboxIDGenerator(), mailbox.DispatchInput{
		RunID:         run.ID,
		TaskID:        task.ID,
		TargetAgentID: "agent-1",
		Payload:       payload,
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	payload["goal"] = "mutated"

	if env.Status != "pending" || env.TaskVersion != task.Version {
		t.Fatalf("Dispatch() envelope contract = %#v", env)
	}
	if env.Payload["goal"] != "collect" || env.ReadSelectors[0].Keys[0] != "brief" || env.WriteTargets[0] != "findings" {
		t.Fatalf("Dispatch() did not copy task dataflow into envelope: %#v", env)
	}
	events, err := uow.Events().ListEvents(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if events[len(events)-1].Type != model.EventTaskDispatched {
		t.Fatalf("last event = %#v", events[len(events)-1])
	}
}

func TestDispatchRejectsUnmetDependencies(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	run, _, err := runsvc.Start(ctx, uow, func(prefix string) string { return prefix + "-seed" }, runsvc.StartInput{
		RunID:      "run-1",
		RootTaskID: "root",
		Request:    "dispatch",
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := uow.Tasks().SaveTask(ctx, model.Task{ID: "dep-1", RunID: run.ID, Status: model.TaskStatusRunning, Version: 1}); err != nil {
		t.Fatalf("SaveTask(dep) error = %v", err)
	}
	if err := uow.Tasks().SaveTask(ctx, model.Task{ID: "task-1", RunID: run.ID, Status: model.TaskStatusWaitingDependency, Version: 1, DependsOn: []string{"dep-1"}}); err != nil {
		t.Fatalf("SaveTask(task) error = %v", err)
	}

	_, err = mailbox.Dispatch(ctx, uow, mailboxIDGenerator(), mailbox.DispatchInput{RunID: run.ID, TaskID: "task-1"})
	if !errors.Is(err, model.ErrDependencyUnmet) {
		t.Fatalf("Dispatch() error = %v, want ErrDependencyUnmet", err)
	}
}

func TestLoadDispatchTargetRejectsTerminalRun(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	if err := uow.Runs().SaveRun(ctx, model.Run{ID: "run-1", Status: model.RunStatusCompleted}); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	if _, _, err := mailbox.LoadDispatchTarget(ctx, uow, "run-1", "task-1"); !errors.Is(err, model.ErrTerminalState) {
		t.Fatalf("LoadDispatchTarget() error = %v, want ErrTerminalState", err)
	}
}
