package execution_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/execution"
	"github.com/Viking602/go-hydaelyn/internal/memory"
	runsvc "github.com/Viking602/go-hydaelyn/internal/run"
)

func executionIDGenerator() execution.IDGenerator {
	next := 0
	return func(prefix string) string {
		next++
		return fmt.Sprintf("%s-%d", prefix, next)
	}
}

func TestAcquireReturnsExistingActiveLease(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	run, root, err := runsvc.Start(ctx, uow, func(prefix string) string { return prefix + "-seed" }, runsvc.StartInput{
		RunID:      "run-1",
		RootTaskID: "root",
		Request:    "execute",
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	root.Status = model.TaskStatusDispatched
	if err := uow.Tasks().SaveTask(ctx, root); err != nil {
		t.Fatalf("SaveTask(root) error = %v", err)
	}
	first, err := execution.Acquire(ctx, uow, executionIDGenerator(), execution.AcquireInput{
		RunID:      run.ID,
		TaskID:     root.ID,
		HolderType: model.HolderComponent,
		HolderID:   "orchestrator",
		TTL:        time.Hour,
	})
	if err != nil {
		t.Fatalf("Acquire(first) error = %v", err)
	}
	second, err := execution.Acquire(ctx, uow, executionIDGenerator(), execution.AcquireInput{
		RunID:      run.ID,
		TaskID:     root.ID,
		HolderType: model.HolderComponent,
		HolderID:   "orchestrator",
		TTL:        time.Hour,
	})
	if err != nil {
		t.Fatalf("Acquire(second) error = %v", err)
	}
	if !first.Acquired || second.Acquired || second.Lease.ID != first.Lease.ID {
		t.Fatalf("Acquire() active lease contract: first=%#v second=%#v", first, second)
	}
}

func TestAcquireSynchronizesLeaseExpiryFields(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	run, root, err := runsvc.Start(ctx, uow, func(prefix string) string { return prefix + "-seed" }, runsvc.StartInput{
		RunID:      "run-expiry",
		RootTaskID: "root",
		Request:    "execute",
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	root.Status = model.TaskStatusDispatched
	if err := uow.Tasks().SaveTask(ctx, root); err != nil {
		t.Fatalf("SaveTask(root) error = %v", err)
	}
	acquired, err := execution.Acquire(ctx, uow, executionIDGenerator(), execution.AcquireInput{
		RunID:      run.ID,
		TaskID:     root.ID,
		HolderType: model.HolderComponent,
		HolderID:   "orchestrator",
		TTL:        time.Hour,
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if acquired.Lease.ExpiresAt.IsZero() || acquired.Lease.Expiry.IsZero() || !acquired.Lease.ExpiresAt.Equal(acquired.Lease.Expiry) {
		t.Fatalf("lease expiry fields not synchronized: %+v", acquired.Lease)
	}
}

func TestHeartbeatSynchronizesLeaseExpiryFields(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	lease := model.TaskExecutionLease{
		ID:         "lease-heartbeat",
		RunID:      "run-heartbeat",
		TaskID:     "task-heartbeat",
		HolderType: model.HolderAgent,
		HolderID:   "agent-1",
		Status:     model.LeaseStatusActive,
		ExpiresAt:  time.Now().Add(time.Minute),
	}
	if err := uow.Leases().SaveLease(ctx, lease); err != nil {
		t.Fatalf("SaveLease() error = %v", err)
	}
	got, err := execution.Heartbeat(ctx, uow, lease.ID, lease.HolderID, time.Hour)
	if err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	if got.ExpiresAt.IsZero() || got.Expiry.IsZero() || !got.ExpiresAt.Equal(got.Expiry) {
		t.Fatalf("heartbeat expiry fields not synchronized: %+v", got)
	}
	if _, err := execution.Heartbeat(ctx, uow, lease.ID, "agent-2", time.Hour); !errors.Is(err, model.ErrLeaseHolderMismatch) {
		t.Fatalf("Heartbeat(wrong holder) error = %v, want ErrLeaseHolderMismatch", err)
	}
}

func TestAcquireReplacesExpiredLeaseWithMonotonicVersion(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	run, root, err := runsvc.Start(ctx, uow, func(prefix string) string { return prefix + "-seed" }, runsvc.StartInput{
		RunID:      "run-takeover",
		RootTaskID: "root",
		Request:    "execute",
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	root.Status = model.TaskStatusDispatched
	root.OwnerComponent = "orchestrator"
	if err := uow.Tasks().SaveTask(ctx, root); err != nil {
		t.Fatalf("SaveTask(root) error = %v", err)
	}
	if err := uow.Leases().SaveLease(ctx, model.TaskExecutionLease{
		ID:         "lease-expired",
		RunID:      run.ID,
		TaskID:     root.ID,
		HolderType: model.HolderComponent,
		HolderID:   "old-worker",
		Status:     model.LeaseStatusActive,
		ExpiresAt:  time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("SaveLease(expired) error = %v", err)
	}

	got, err := execution.Acquire(ctx, uow, executionIDGenerator(), execution.AcquireInput{
		RunID:      run.ID,
		TaskID:     root.ID,
		HolderType: model.HolderComponent,
		HolderID:   "orchestrator",
		TTL:        time.Minute,
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if !got.Acquired || got.Lease.ID == "lease-expired" || got.Lease.Version != 2 {
		t.Fatalf("Acquire() takeover = %#v, want new lease at version 2", got)
	}
}

func TestValidateSubmissionAcceptsLegacyExpiryField(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	if err := uow.Runs().SaveRun(ctx, model.Run{ID: "run-legacy", Status: model.RunStatusRunning}); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	if err := uow.Tasks().SaveTask(ctx, model.Task{ID: "task-legacy", RunID: "run-legacy", Status: model.TaskStatusRunning, Version: 1}); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}
	if err := uow.Leases().SaveLease(ctx, model.TaskExecutionLease{
		ID:          "lease-legacy",
		RunID:       "run-legacy",
		TaskID:      "task-legacy",
		HolderType:  model.HolderAgent,
		HolderID:    "agent-1",
		TaskVersion: 1,
		Status:      model.LeaseStatusActive,
		Expiry:      time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("SaveLease() error = %v", err)
	}
	if _, _, _, err := execution.ValidateSubmission(ctx, uow, "run-legacy", "task-legacy", "lease-legacy", model.HolderAgent, "agent-1", 1); err != nil {
		t.Fatalf("ValidateSubmission() error = %v", err)
	}
}

func TestAcquireRejectsStaleEnvelopeVersion(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	run, root, err := runsvc.Start(ctx, uow, func(prefix string) string { return prefix + "-seed" }, runsvc.StartInput{
		RunID:      "run-1",
		RootTaskID: "root",
		Request:    "execute",
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := uow.MailboxOutbox().QueueEnvelope(ctx, model.TaskEnvelope{
		ID:            "env-1",
		RunID:         run.ID,
		TaskID:        root.ID,
		TargetAgentID: "agent-1",
		TaskVersion:   root.Version + 1,
		Status:        "pending",
	}); err != nil {
		t.Fatalf("QueueEnvelope() error = %v", err)
	}

	_, err = execution.Acquire(ctx, uow, executionIDGenerator(), execution.AcquireInput{
		RunID:      run.ID,
		TaskID:     root.ID,
		EnvelopeID: "env-1",
		HolderType: model.HolderAgent,
		HolderID:   "agent-1",
	})
	if !errors.Is(err, model.ErrStaleTaskVersion) {
		t.Fatalf("Acquire() error = %v, want ErrStaleTaskVersion", err)
	}
}
