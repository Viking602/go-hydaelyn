package report

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Viking602/venat/api"
	commandbus "github.com/Viking602/venat/internal/command"
	"github.com/Viking602/venat/internal/core/ports"
	"github.com/Viking602/venat/internal/memory"
)

func TestSubmitTypedReportCompletesTaskAndReleasesLease(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	saveReportFixture(ctx, t, uow, api.Task{CompletionCriteria: []string{"accepted"}})

	bus := commandbus.NewBus()
	RegisterHandlers(bus, HandlerOptions{NewID: func(prefix string) string { return prefix + "-1" }})
	result, err := bus.Execute(ctx, uow, SubmitTypedCommand{
		RunID:       "run-1",
		TaskID:      "task-1",
		LeaseID:     "lease-1",
		HolderType:  api.HolderAgent,
		HolderID:    "agent-1",
		TaskVersion: 1,
		Report: api.TypedReport{
			Status:  api.ReportStatusSuccess,
			Summary: "accepted result",
		},
	})
	if err != nil {
		t.Fatalf("SubmitTypedReport error = %v", err)
	}
	submitted := result.(SubmitTypedResult)
	if len(submitted.Tasks) != 1 || submitted.Tasks[0].Status != api.TaskStatusCompleted || submitted.Tasks[0].Version != 2 {
		t.Fatalf("submitted tasks = %#v", submitted.Tasks)
	}
	if len(submitted.Leases) != 1 || submitted.Leases[0].Status != api.LeaseStatusReleased {
		t.Fatalf("submitted leases = %#v", submitted.Leases)
	}
	if len(submitted.Events) < 3 || submitted.Events[0].Type != api.EventTypedReportSubmitted || submitted.Events[len(submitted.Events)-1].Type != api.EventTaskCompleted {
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
	saveReportFixture(ctx, t, uow, api.Task{CompletionCriteria: []string{"must mention audit"}})

	bus := commandbus.NewBus()
	RegisterHandlers(bus, HandlerOptions{NewID: func(prefix string) string { return prefix + "-1" }})
	_, err = bus.Execute(ctx, uow, SubmitTypedCommand{
		RunID:       "run-1",
		TaskID:      "task-1",
		LeaseID:     "lease-1",
		HolderType:  api.HolderAgent,
		HolderID:    "agent-1",
		TaskVersion: 1,
		Report: api.TypedReport{
			Status:  api.ReportStatusSuccess,
			Summary: "accepted result",
		},
	})
	if !errors.Is(err, api.ErrCompletionCriteriaUnmet) {
		t.Fatalf("SubmitTypedReport error = %v, want ErrCompletionCriteriaUnmet", err)
	}
}

func TestRetryBackoffIsExponentialAndOverflowSafe(t *testing.T) {
	base := 250 * time.Millisecond
	for _, test := range []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 1, want: base},
		{attempt: 2, want: 2 * base},
		{attempt: 4, want: 8 * base},
		{attempt: 0, want: base},
	} {
		if got := retryBackoff(base, 0, test.attempt); got != test.want {
			t.Fatalf("retryBackoff(%s, 0, %d) = %s, want %s", base, test.attempt, got, test.want)
		}
	}
	if got := retryBackoff(time.Duration(1<<62), 0, 2); got != time.Duration(1<<63-1) {
		t.Fatalf("overflow-safe retryBackoff = %s", got)
	}
	if got := retryBackoff(base, 3*base, 4); got != 3*base {
		t.Fatalf("capped retryBackoff = %s, want %s", got, 3*base)
	}
}

func saveReportFixture(ctx context.Context, t *testing.T, uow ports.UnitOfWork, taskOverride api.Task) {
	t.Helper()
	task := api.Task{
		ID:           "task-1",
		RunID:        "run-1",
		Status:       api.TaskStatusRunning,
		Version:      1,
		OwnerAgentID: "agent-1",
	}
	task.CompletionCriteria = taskOverride.CompletionCriteria
	if err := uow.Runs().SaveRun(ctx, api.Run{ID: "run-1", RootTaskID: "task-1", Status: api.RunStatusRunning}); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	if err := uow.Tasks().SaveTask(ctx, task); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}
	if err := uow.Leases().SaveLease(ctx, api.TaskExecutionLease{ID: "lease-1", RunID: "run-1", TaskID: "task-1", HolderType: api.HolderAgent, HolderID: "agent-1", TaskVersion: 1, Status: api.LeaseStatusActive, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("SaveLease() error = %v", err)
	}
}
