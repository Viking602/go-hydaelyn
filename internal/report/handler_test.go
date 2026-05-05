package report

import (
	"context"
	"errors"
	"testing"
	"time"

	commandbus "github.com/Viking602/go-hydaelyn/internal/command"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
	"github.com/Viking602/go-hydaelyn/internal/memory"
)

func TestSubmitTypedReportCompletesTaskAndReleasesLease(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	saveReportFixture(ctx, t, uow, model.Task{CompletionCriteria: []string{"accepted"}})

	bus := commandbus.NewBus()
	RegisterHandlers(bus, HandlerOptions{NewID: func(prefix string) string { return prefix + "-1" }})
	result, err := bus.Execute(ctx, uow, SubmitTypedCommand{
		RunID:       "run-1",
		TaskID:      "task-1",
		LeaseID:     "lease-1",
		HolderType:  model.HolderAgent,
		HolderID:    "agent-1",
		TaskVersion: 1,
		Report: model.TypedReport{
			Status:  model.ReportStatusSuccess,
			Summary: "accepted result",
		},
	})
	if err != nil {
		t.Fatalf("SubmitTypedReport error = %v", err)
	}
	submitted := result.(SubmitTypedResult)
	if len(submitted.Tasks) != 1 || submitted.Tasks[0].Status != model.TaskStatusCompleted || submitted.Tasks[0].Version != 2 {
		t.Fatalf("submitted tasks = %#v", submitted.Tasks)
	}
	if len(submitted.Leases) != 1 || submitted.Leases[0].Status != model.LeaseStatusReleased {
		t.Fatalf("submitted leases = %#v", submitted.Leases)
	}
	if len(submitted.Events) < 3 || submitted.Events[0].Type != model.EventTypedReportSubmitted || submitted.Events[len(submitted.Events)-1].Type != model.EventTaskCompleted {
		t.Fatalf("submitted events = %#v", submitted.Events)
	}
}

func TestSubmitTypedReportRejectsUnmetCompletionCriteria(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	saveReportFixture(ctx, t, uow, model.Task{CompletionCriteria: []string{"must mention audit"}})

	bus := commandbus.NewBus()
	RegisterHandlers(bus, HandlerOptions{NewID: func(prefix string) string { return prefix + "-1" }})
	_, err = bus.Execute(ctx, uow, SubmitTypedCommand{
		RunID:       "run-1",
		TaskID:      "task-1",
		LeaseID:     "lease-1",
		HolderType:  model.HolderAgent,
		HolderID:    "agent-1",
		TaskVersion: 1,
		Report: model.TypedReport{
			Status:  model.ReportStatusSuccess,
			Summary: "accepted result",
		},
	})
	if !errors.Is(err, model.ErrCompletionCriteriaUnmet) {
		t.Fatalf("SubmitTypedReport error = %v, want ErrCompletionCriteriaUnmet", err)
	}
}

func saveReportFixture(ctx context.Context, t *testing.T, uow ports.UnitOfWork, taskOverride model.Task) {
	t.Helper()
	task := model.Task{
		ID:           "task-1",
		RunID:        "run-1",
		Status:       model.TaskStatusRunning,
		Version:      1,
		OwnerAgentID: "agent-1",
	}
	task.CompletionCriteria = taskOverride.CompletionCriteria
	if err := uow.Runs().SaveRun(ctx, model.Run{ID: "run-1", RootTaskID: "task-1", Status: model.RunStatusRunning}); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	if err := uow.Tasks().SaveTask(ctx, task); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}
	if err := uow.Leases().SaveLease(ctx, model.TaskExecutionLease{ID: "lease-1", RunID: "run-1", TaskID: "task-1", HolderType: model.HolderAgent, HolderID: "agent-1", TaskVersion: 1, Status: model.LeaseStatusActive, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("SaveLease() error = %v", err)
	}
}
