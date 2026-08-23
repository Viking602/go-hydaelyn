package execution_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/execution"
	"github.com/Viking602/venat/internal/memory"
	runsvc "github.com/Viking602/venat/internal/run"
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
	root.Status = api.TaskStatusDispatched
	if err := uow.Tasks().SaveTask(ctx, root); err != nil {
		t.Fatalf("SaveTask(root) error = %v", err)
	}
	first, err := execution.Acquire(ctx, uow, executionIDGenerator(), execution.AcquireInput{
		RunID:      run.ID,
		TaskID:     root.ID,
		HolderType: api.HolderComponent,
		HolderID:   "orchestrator",
		TTL:        time.Hour,
	})
	if err != nil {
		t.Fatalf("Acquire(first) error = %v", err)
	}
	second, err := execution.Acquire(ctx, uow, executionIDGenerator(), execution.AcquireInput{
		RunID:      run.ID,
		TaskID:     root.ID,
		HolderType: api.HolderComponent,
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
	root.Status = api.TaskStatusDispatched
	if err := uow.Tasks().SaveTask(ctx, root); err != nil {
		t.Fatalf("SaveTask(root) error = %v", err)
	}
	acquired, err := execution.Acquire(ctx, uow, executionIDGenerator(), execution.AcquireInput{
		RunID:      run.ID,
		TaskID:     root.ID,
		HolderType: api.HolderComponent,
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
	lease := api.TaskExecutionLease{
		ID:         "lease-heartbeat",
		RunID:      "run-heartbeat",
		TaskID:     "task-heartbeat",
		HolderType: api.HolderAgent,
		HolderID:   "agent-1",
		Status:     api.LeaseStatusActive,
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
	if _, err := execution.Heartbeat(ctx, uow, lease.ID, "agent-2", time.Hour); !errors.Is(err, api.ErrLeaseHolderMismatch) {
		t.Fatalf("Heartbeat(wrong holder) error = %v, want ErrLeaseHolderMismatch", err)
	}
}

func TestReleaseIsIdempotent(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	lease := api.TaskExecutionLease{
		ID:         "lease-release",
		RunID:      "run-release",
		TaskID:     "task-release",
		HolderType: api.HolderAgent,
		HolderID:   "agent-1",
		Status:     api.LeaseStatusActive,
	}
	if err := uow.Leases().SaveLease(ctx, lease); err != nil {
		t.Fatalf("SaveLease() error = %v", err)
	}
	if _, err := execution.Release(ctx, uow, lease.ID, lease.HolderID); err != nil {
		t.Fatalf("Release(first) error = %v", err)
	}
	if _, err := execution.Release(ctx, uow, lease.ID, lease.HolderID); err != nil {
		t.Fatalf("Release(second) error = %v", err)
	}
	events, err := uow.Events().ListEvents(ctx, lease.RunID)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 1 || events[0].Type != api.EventTaskExecutionReleased {
		t.Fatalf("release events = %#v, want one TaskExecutionReleased", events)
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
	root.Status = api.TaskStatusDispatched
	root.OwnerComponent = "orchestrator"
	if err := uow.Tasks().SaveTask(ctx, root); err != nil {
		t.Fatalf("SaveTask(root) error = %v", err)
	}
	if err := uow.Leases().SaveLease(ctx, api.TaskExecutionLease{
		ID:         "lease-expired",
		RunID:      run.ID,
		TaskID:     root.ID,
		HolderType: api.HolderComponent,
		HolderID:   "old-worker",
		Status:     api.LeaseStatusActive,
		ExpiresAt:  time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("SaveLease(expired) error = %v", err)
	}

	got, err := execution.Acquire(ctx, uow, executionIDGenerator(), execution.AcquireInput{
		RunID:      run.ID,
		TaskID:     root.ID,
		HolderType: api.HolderComponent,
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
	if err := uow.Runs().SaveRun(ctx, api.Run{ID: "run-legacy", Status: api.RunStatusRunning}); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	if err := uow.Tasks().SaveTask(ctx, api.Task{ID: "task-legacy", RunID: "run-legacy", Status: api.TaskStatusRunning, Version: 1}); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}
	if err := uow.Leases().SaveLease(ctx, api.TaskExecutionLease{
		ID:          "lease-legacy",
		RunID:       "run-legacy",
		TaskID:      "task-legacy",
		HolderType:  api.HolderAgent,
		HolderID:    "agent-1",
		TaskVersion: 1,
		Status:      api.LeaseStatusActive,
		Expiry:      time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("SaveLease() error = %v", err)
	}
	if _, _, _, err := execution.ValidateSubmission(ctx, uow, "run-legacy", "task-legacy", "lease-legacy", api.HolderAgent, "agent-1", 1); err != nil {
		t.Fatalf("ValidateSubmission() error = %v", err)
	}
}

func TestValidateSubmissionRejectsExpiredLease(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	if err := uow.Runs().SaveRun(ctx, api.Run{ID: "run-expired", Status: api.RunStatusRunning}); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	if err := uow.Tasks().SaveTask(ctx, api.Task{ID: "task-expired", RunID: "run-expired", Status: api.TaskStatusRunning, Version: 1}); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}
	if err := uow.Leases().SaveLease(ctx, api.TaskExecutionLease{
		ID: "lease-expired-submit", RunID: "run-expired", TaskID: "task-expired",
		HolderType: api.HolderAgent, HolderID: "agent-1", TaskVersion: 1,
		Status: api.LeaseStatusActive, ExpiresAt: time.Now().Add(-time.Second),
	}); err != nil {
		t.Fatalf("SaveLease() error = %v", err)
	}
	if _, _, _, err := execution.ValidateSubmission(
		ctx, uow, "run-expired", "task-expired", "lease-expired-submit", api.HolderAgent, "agent-1", 1,
	); !errors.Is(err, api.ErrLeaseNotActive) {
		t.Fatalf("ValidateSubmission() error = %v, want ErrLeaseNotActive", err)
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
	if err := uow.MailboxOutbox().QueueEnvelope(ctx, api.TaskEnvelope{
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
		HolderType: api.HolderAgent,
		HolderID:   "agent-1",
	})
	if !errors.Is(err, api.ErrStaleTaskVersion) {
		t.Fatalf("Acquire() error = %v, want ErrStaleTaskVersion", err)
	}
}
