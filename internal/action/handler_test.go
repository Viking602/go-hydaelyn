package action_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Viking602/venat/internal/action"
	commandbus "github.com/Viking602/venat/internal/command"
	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
	"github.com/Viking602/venat/internal/memory"
)

func TestStartActionAttemptRequiresActionCapableTask(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	saveActionFixture(ctx, t, uow, false)

	bus := commandbus.NewBus()
	action.RegisterHandlers(bus, action.HandlerOptions{NewID: func(string) string { return "attempt-generated" }})

	_, err = bus.Execute(ctx, uow, action.StartActionAttemptCommand{
		RunID:       "run-1",
		TaskID:      "task-1",
		LeaseID:     "lease-1",
		HolderType:  model.HolderAgent,
		HolderID:    "agent-1",
		TaskVersion: 1,
		ToolName:    "deploy",
	})
	if !errors.Is(err, model.ErrActionTaskRequired) {
		t.Fatalf("StartActionAttempt error = %v, want ErrActionTaskRequired", err)
	}
}

func TestStartActionAttemptPersistsAttemptAndEvent(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	saveActionFixture(ctx, t, uow, true)

	bus := commandbus.NewBus()
	action.RegisterHandlers(bus, action.HandlerOptions{NewID: func(string) string { return "attempt-generated" }})

	result, err := bus.Execute(ctx, uow, action.StartActionAttemptCommand{
		ActionID:       "action-1",
		RunID:          "run-1",
		TaskID:         "task-1",
		LeaseID:        "lease-1",
		HolderType:     model.HolderAgent,
		HolderID:       "agent-1",
		TaskVersion:    1,
		ToolName:       "deploy",
		IdempotencyKey: "idem-1",
		InputHash:      "hash-1",
	})
	if err != nil {
		t.Fatalf("StartActionAttempt error = %v", err)
	}
	attempt := result.(model.ActionAttempt)
	if attempt.AttemptID != "attempt-generated" || attempt.Status != model.ActionAttemptRunning || attempt.ToolName != "deploy" {
		t.Fatalf("attempt = %#v", attempt)
	}
	events, err := uow.Events().ListEvents(ctx, "run-1")
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 1 || events[0].Type != model.EventActionAttemptStarted {
		t.Fatalf("events = %#v", events)
	}
}

func TestStartActionAttemptReturnsExistingForSameIdempotencyKey(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	saveActionFixture(ctx, t, uow, true)

	nextID := 0
	bus := commandbus.NewBus()
	action.RegisterHandlers(bus, action.HandlerOptions{NewID: func(string) string {
		nextID++
		if nextID == 1 {
			return "attempt-first"
		}
		return "attempt-second"
	}})

	cmd := action.StartActionAttemptCommand{
		ActionID:       "action-1",
		RunID:          "run-1",
		TaskID:         "task-1",
		LeaseID:        "lease-1",
		HolderType:     model.HolderAgent,
		HolderID:       "agent-1",
		TaskVersion:    1,
		ToolName:       "deploy",
		IdempotencyKey: "idem-1",
		InputHash:      "hash-1",
	}
	first, err := bus.Execute(ctx, uow, cmd)
	if err != nil {
		t.Fatalf("first StartActionAttempt error = %v", err)
	}
	second, err := bus.Execute(ctx, uow, cmd)
	if err != nil {
		t.Fatalf("second StartActionAttempt error = %v", err)
	}
	if first.(model.ActionAttempt).AttemptID != second.(model.ActionAttempt).AttemptID {
		t.Fatalf("expected existing attempt, got first=%#v second=%#v", first, second)
	}
	events, err := uow.Events().ListEvents(ctx, "run-1")
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected duplicate idempotency start to avoid duplicate event, got %d events", len(events))
	}
}

func TestStartActionAttemptRejectsSameIdempotencyKeyWithDifferentInputHash(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	saveActionFixture(ctx, t, uow, true)

	nextID := 0
	bus := commandbus.NewBus()
	action.RegisterHandlers(bus, action.HandlerOptions{NewID: func(string) string {
		nextID++
		if nextID == 1 {
			return "attempt-first"
		}
		return "attempt-second"
	}})

	base := action.StartActionAttemptCommand{
		ActionID:       "action-1",
		RunID:          "run-1",
		TaskID:         "task-1",
		LeaseID:        "lease-1",
		HolderType:     model.HolderAgent,
		HolderID:       "agent-1",
		TaskVersion:    1,
		ToolName:       "deploy",
		IdempotencyKey: "idem-1",
		InputHash:      "hash-1",
	}
	if _, err := bus.Execute(ctx, uow, base); err != nil {
		t.Fatalf("first StartActionAttempt error = %v", err)
	}
	base.InputHash = "hash-2"
	_, err = bus.Execute(ctx, uow, base)
	if !errors.Is(err, model.ErrIdempotencyConflict) {
		t.Fatalf("expected ErrIdempotencyConflict, got %v", err)
	}
}

func TestStartActionAttemptWithoutIdempotencyKeyCreatesDistinctAttempts(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	saveActionFixture(ctx, t, uow, true)

	nextID := 0
	bus := commandbus.NewBus()
	action.RegisterHandlers(bus, action.HandlerOptions{NewID: func(string) string {
		nextID++
		if nextID == 1 {
			return "attempt-first"
		}
		return "attempt-second"
	}})

	cmd := action.StartActionAttemptCommand{
		ActionID:    "action-1",
		RunID:       "run-1",
		TaskID:      "task-1",
		LeaseID:     "lease-1",
		HolderType:  model.HolderAgent,
		HolderID:    "agent-1",
		TaskVersion: 1,
		ToolName:    "deploy",
	}
	first, err := bus.Execute(ctx, uow, cmd)
	if err != nil {
		t.Fatalf("first StartActionAttempt error = %v", err)
	}
	second, err := bus.Execute(ctx, uow, cmd)
	if err != nil {
		t.Fatalf("second StartActionAttempt error = %v", err)
	}
	if first.(model.ActionAttempt).AttemptID == second.(model.ActionAttempt).AttemptID {
		t.Fatalf("expected distinct attempts without idempotency key, got first=%#v second=%#v", first, second)
	}
}

func saveActionFixture(ctx context.Context, t *testing.T, uow ports.UnitOfWork, allowsAction bool) {
	t.Helper()
	if err := uow.Runs().SaveRun(ctx, model.Run{ID: "run-1", RootTaskID: "task-1", Status: model.RunStatusRunning}); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	if err := uow.Tasks().SaveTask(ctx, model.Task{ID: "task-1", RunID: "run-1", Status: model.TaskStatusRunning, Version: 1, OwnerAgentID: "agent-1", AllowsAction: allowsAction}); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}
	if err := uow.Leases().SaveLease(ctx, model.TaskExecutionLease{ID: "lease-1", RunID: "run-1", TaskID: "task-1", HolderType: model.HolderAgent, HolderID: "agent-1", TaskVersion: 1, Status: model.LeaseStatusActive, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("SaveLease() error = %v", err)
	}
}
