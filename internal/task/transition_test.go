package task_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/memory"
	tasksvc "github.com/Viking602/venat/internal/task"
)

func TestPureTaskTransitionCanBumpVersion(t *testing.T) {
	task := api.Task{
		ID:      "task-1",
		RunID:   "run-1",
		Status:  api.TaskStatusDispatched,
		Version: 3,
	}
	next, err := tasksvc.PureTaskTransition(task, api.TaskStatusRunning, true)
	if err != nil {
		t.Fatalf("PureTaskTransition() error = %v", err)
	}
	if next.Status != api.TaskStatusRunning || next.Version != 4 || next.UpdatedAt.IsZero() {
		t.Fatalf("PureTaskTransition() = %#v", next)
	}
}

func TestPureTaskTransitionRejectsTerminalTask(t *testing.T) {
	_, err := tasksvc.PureTaskTransition(api.Task{
		ID:      "task-1",
		RunID:   "run-1",
		Status:  api.TaskStatusCompleted,
		Version: 1,
	}, api.TaskStatusRunning, true)
	if !errors.Is(err, api.ErrTerminalState) {
		t.Fatalf("PureTaskTransition() error = %v, want ErrTerminalState", err)
	}
}

func TestTransitionRunPersistsRunAndEmitsStatusEvent(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	run := api.Run{ID: "run-1", RootTaskID: "task-1", Status: api.RunStatusCreated}
	if err := uow.Runs().SaveRun(ctx, run); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}

	next, changed, err := tasksvc.TransitionRun(ctx, uow, run.ID, api.RunStatusPlanning)
	if err != nil {
		t.Fatalf("TransitionRun() error = %v", err)
	}
	if !changed || next.Status != api.RunStatusPlanning {
		t.Fatalf("TransitionRun() = (%#v, %v), want planning change", next, changed)
	}
	persisted, err := uow.Runs().LoadRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("LoadRun() error = %v", err)
	}
	if persisted.Status != api.RunStatusPlanning {
		t.Fatalf("persisted run status = %q", persisted.Status)
	}
	events, err := uow.Events().ListEvents(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 1 || events[0].Type != api.EventRunStatusChanged || events[0].TaskID != run.RootTaskID {
		t.Fatalf("TransitionRun() events = %#v", events)
	}
}

func TestTransitionTaskPersistsVersionBump(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	task := api.Task{ID: "task-1", RunID: "run-1", Status: api.TaskStatusDispatched, Version: 7}
	if err := uow.Tasks().SaveTask(ctx, task); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}

	next, err := tasksvc.TransitionTask(ctx, uow, task.RunID, task.ID, api.TaskStatusRunning, true)
	if err != nil {
		t.Fatalf("TransitionTask() error = %v", err)
	}
	if next.Status != api.TaskStatusRunning || next.Version != 8 {
		t.Fatalf("TransitionTask() = %#v", next)
	}
	persisted, err := uow.Tasks().LoadTask(ctx, task.RunID, task.ID)
	if err != nil {
		t.Fatalf("LoadTask() error = %v", err)
	}
	if persisted.Status != api.TaskStatusRunning || persisted.Version != 8 {
		t.Fatalf("persisted task = %#v", persisted)
	}
}
