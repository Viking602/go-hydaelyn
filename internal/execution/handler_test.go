package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
	"github.com/Viking602/venat/internal/memory"
)

func TestAppendCheckpointEnforcesPerTaskCountLimitWithoutMutation(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	run := model.Run{ID: "run-checkpoint-limit", Status: model.RunStatusRunning}
	task := model.Task{ID: "task-checkpoint-limit", RunID: run.ID, Status: model.TaskStatusRunning, Version: 1}
	lease := model.TaskExecutionLease{
		ID: "lease-checkpoint-limit", RunID: run.ID, TaskID: task.ID,
		HolderType: model.HolderAgent, HolderID: "agent-1", TaskVersion: task.Version,
		Status: model.LeaseStatusActive, ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := uow.Runs().SaveRun(ctx, run); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	if err := uow.Tasks().SaveTask(ctx, task); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}
	if err := uow.Leases().SaveLease(ctx, lease); err != nil {
		t.Fatalf("SaveLease() error = %v", err)
	}
	for index := range maxExecutionCheckpointCount - 1 {
		if err := uow.Events().AppendEvent(ctx, model.Event{
			RunID: run.ID, TaskID: task.ID, Type: model.EventExecutionCheckpointed,
			Payload: map[string]any{"record": map[string]any{"index": index}},
		}); err != nil {
			t.Fatalf("AppendEvent(%d) error = %v", index, err)
		}
	}

	handler := appendTaskExecutionEventHandler{}
	command := AppendTaskExecutionEventCommand{
		RunID: run.ID, TaskID: task.ID, LeaseID: lease.ID,
		HolderType: lease.HolderType, HolderID: lease.HolderID, TaskVersion: task.Version,
		Event: model.Event{
			RunID: run.ID, TaskID: task.ID, Type: model.EventExecutionCheckpointed,
			Payload: map[string]any{"record": map[string]any{"index": maxExecutionCheckpointCount - 1}},
		},
	}
	if _, err := handler.Handle(ctx, uow, command); err != nil {
		t.Fatalf("append checkpoint at limit error = %v", err)
	}
	if _, err := handler.Handle(ctx, uow, command); !errors.Is(err, model.ErrCheckpointLimitExceeded) {
		t.Fatalf("append checkpoint past limit error = %v, want ErrCheckpointLimitExceeded", err)
	}
	events, err := uow.Events().ListEvents(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != maxExecutionCheckpointCount {
		t.Fatalf("event count after rejected append = %d, want %d", len(events), maxExecutionCheckpointCount)
	}
}

func TestAppendTaskExecutionEventRejectsLifecycleEvent(t *testing.T) {
	ctx, uow, command := executionEventFixture(t)
	command.Event = model.Event{
		RunID: command.RunID, TaskID: command.TaskID, Type: model.EventTaskCompleted,
	}

	if _, err := (appendTaskExecutionEventHandler{}).Handle(ctx, uow, command); !errors.Is(err, model.ErrInvalidCommand) {
		t.Fatalf("AppendTaskExecutionEvent error = %v, want ErrInvalidCommand", err)
	}
	events, err := uow.Events().ListEvents(ctx, command.RunID)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("rejected lifecycle event was appended: %#v", events)
	}
}

func TestAppendTaskExecutionEventAssignsStoreSequence(t *testing.T) {
	ctx, uow, command := executionEventFixture(t)
	command.Event = model.Event{
		RunID: command.RunID, TaskID: command.TaskID, Sequence: 99,
		Type: model.EventType("StepCompleted"), Payload: map[string]any{"record": "step"},
	}

	if _, err := (appendTaskExecutionEventHandler{}).Handle(ctx, uow, command); err != nil {
		t.Fatalf("AppendTaskExecutionEvent error = %v", err)
	}
	events, err := uow.Events().ListEvents(ctx, command.RunID)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 1 || events[0].Sequence != 1 {
		t.Fatalf("stored events = %#v, want one store-assigned sequence", events)
	}
}

func executionEventFixture(t *testing.T) (context.Context, ports.UnitOfWork, AppendTaskExecutionEventCommand) {
	t.Helper()
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	t.Cleanup(func() { _ = uow.Rollback(ctx) })

	run := model.Run{ID: "run-execution-event", Status: model.RunStatusRunning}
	task := model.Task{ID: "task-execution-event", RunID: run.ID, Status: model.TaskStatusRunning, Version: 1}
	lease := model.TaskExecutionLease{
		ID: "lease-execution-event", RunID: run.ID, TaskID: task.ID,
		HolderType: model.HolderAgent, HolderID: "agent-1", TaskVersion: task.Version,
		Status: model.LeaseStatusActive, ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := uow.Runs().SaveRun(ctx, run); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	if err := uow.Tasks().SaveTask(ctx, task); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}
	if err := uow.Leases().SaveLease(ctx, lease); err != nil {
		t.Fatalf("SaveLease() error = %v", err)
	}
	return ctx, uow, AppendTaskExecutionEventCommand{
		RunID: run.ID, TaskID: task.ID, LeaseID: lease.ID,
		HolderType: lease.HolderType, HolderID: lease.HolderID, TaskVersion: task.Version,
	}
}
