package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

func TestMemoryUnitOfWorkRollbackRunStore(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := uow.Runs().SaveRun(ctx, model.Run{ID: "run-1", Status: model.RunStatusCreated}); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	if err := uow.Rollback(ctx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	reader, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() reader error = %v", err)
	}
	defer func() { _ = reader.Rollback(ctx) }()
	if _, err := reader.Runs().LoadRun(ctx, "run-1"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("LoadRun() after rollback error = %v, want ErrNotFound", err)
	}
}

func TestMemoryUnitOfWorkCommitRunStore(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := uow.Runs().SaveRun(ctx, model.Run{ID: "run-1", Status: model.RunStatusCreated}); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	if err := uow.Commit(ctx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	reader, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() reader error = %v", err)
	}
	defer func() { _ = reader.Rollback(ctx) }()
	if _, err := reader.Runs().LoadRun(ctx, "run-1"); err != nil {
		t.Fatalf("LoadRun() after commit error = %v", err)
	}
}

func TestMemoryUnitOfWorkRollbackLeaseStore(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	lease := model.TaskExecutionLease{ID: "lease-1", RunID: "run-1", TaskID: "task-1", Status: model.LeaseStatusActive}
	if err := uow.Leases().SaveLease(ctx, lease); err != nil {
		t.Fatalf("SaveLease() error = %v", err)
	}
	if err := uow.Rollback(ctx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	reader, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() reader error = %v", err)
	}
	defer func() { _ = reader.Rollback(ctx) }()
	if _, err := reader.Leases().LoadLease(ctx, "lease-1"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("LoadLease() after rollback error = %v, want ErrNotFound", err)
	}
}

func TestBlackboardSubscriberNotNotifiedOnRollback(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	ch, cancel, err := provider.Subscribe(ctx, "run-1", model.BlackboardSelector{})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer func() { _ = cancel() }()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := uow.Blackboard().WriteItem(ctx, model.BlackboardItem{RunID: "run-1", Source: model.SourceIdentity{Type: model.SourceSystem, ID: "test"}}); err != nil {
		t.Fatalf("WriteItem() error = %v", err)
	}
	if err := uow.Rollback(ctx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	select {
	case item := <-ch:
		t.Fatalf("unexpected notification after rollback: %#v", item)
	case <-time.After(30 * time.Millisecond):
	}
}

func TestBlackboardSubscriberNotifiedAfterCommit(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	ch, cancel, err := provider.Subscribe(ctx, "run-1", model.BlackboardSelector{})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer func() { _ = cancel() }()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := uow.Blackboard().WriteItem(ctx, model.BlackboardItem{ID: "bb-1", RunID: "run-1", Source: model.SourceIdentity{Type: model.SourceSystem, ID: "test"}}); err != nil {
		t.Fatalf("WriteItem() error = %v", err)
	}
	if err := uow.Commit(ctx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	select {
	case item := <-ch:
		if item.ID != "bb-1" {
			t.Fatalf("notification item ID = %q", item.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for notification")
	}
}

func TestMemoryProviderBeginRespectsContextWhileWaitingForTransaction(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	_, err = provider.Begin(waitCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Begin(waitCtx) error = %v, want DeadlineExceeded", err)
	}
}

func TestMemoryProviderCommittedReadDoesNotWaitForActiveWriteTransaction(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	done := make(chan struct{})
	go func() {
		_ = provider.CommittedSnapshot()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("CommittedSnapshot blocked behind active write transaction")
	}
}

func TestMemoryProviderBeginReturnsUnifiedUnitOfWork(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()

	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}

	run := model.Run{ID: "run-unified", Status: model.RunStatusCreated}
	task := model.Task{ID: "task-unified", RunID: run.ID, Status: model.TaskStatusCreated, Version: 1}
	lease := model.TaskExecutionLease{ID: "lease-unified", RunID: run.ID, TaskID: task.ID, Status: model.LeaseStatusActive}
	approval := model.ApprovalRequest{ApprovalID: "approval-unified", RunID: run.ID, TaskID: task.ID, Status: "pending"}
	token := model.ResumeToken{TokenID: "token-unified", RunID: run.ID, TaskID: task.ID, ApprovalID: approval.ApprovalID}
	attempt := model.ActionAttempt{AttemptID: "attempt-unified", RunID: run.ID, TaskID: task.ID, Status: model.ActionAttemptRunning}

	if err := uow.Runs().SaveRun(ctx, run); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	if err := uow.Tasks().SaveTask(ctx, task); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: run.ID, TaskID: task.ID, Type: model.EventTaskCreated}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	if err := uow.Blackboard().WriteItem(ctx, model.BlackboardItem{ID: "bb-unified", RunID: run.ID, TaskID: task.ID, Source: model.SourceIdentity{Type: model.SourceSystem, ID: "test"}}); err != nil {
		t.Fatalf("WriteItem() error = %v", err)
	}
	if err := uow.MailboxOutbox().QueueEnvelope(ctx, model.TaskEnvelope{ID: "env-unified", RunID: run.ID, TaskID: task.ID}); err != nil {
		t.Fatalf("QueueEnvelope() error = %v", err)
	}
	if err := uow.UserMessages().QueueMessage(ctx, model.UserMessage{ID: "msg-unified", RunID: run.ID, TaskID: task.ID}); err != nil {
		t.Fatalf("QueueMessage() error = %v", err)
	}
	if err := uow.Trace().SaveTraceSpan(ctx, model.TraceSpan{ID: "span-unified", RunID: run.ID, TaskID: task.ID, Name: "unified", Status: model.TraceSpanStarted}); err != nil {
		t.Fatalf("SaveTraceSpan() error = %v", err)
	}
	if err := uow.Leases().SaveLease(ctx, lease); err != nil {
		t.Fatalf("SaveLease() error = %v", err)
	}
	if err := uow.Approvals().SaveApproval(ctx, approval); err != nil {
		t.Fatalf("SaveApproval() error = %v", err)
	}
	if err := uow.ResumeTokens().SaveResumeToken(ctx, token); err != nil {
		t.Fatalf("SaveResumeToken() error = %v", err)
	}
	if err := uow.ActionAttempts().SaveActionAttempt(ctx, attempt); err != nil {
		t.Fatalf("SaveActionAttempt() error = %v", err)
	}
	if err := uow.Commit(ctx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	reader, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() reader error = %v", err)
	}
	defer func() { _ = reader.Rollback(ctx) }()

	if _, err := reader.Runs().LoadRun(ctx, run.ID); err != nil {
		t.Fatalf("LoadRun() error = %v", err)
	}
	if _, err := reader.Tasks().LoadTask(ctx, run.ID, task.ID); err != nil {
		t.Fatalf("LoadTask() error = %v", err)
	}
	if events, err := reader.Events().ListEvents(ctx, run.ID); err != nil || len(events) != 1 {
		t.Fatalf("ListEvents() = %#v, %v", events, err)
	}
	if items, err := reader.Blackboard().SelectItems(ctx, run.ID, model.BlackboardSelector{}); err != nil || len(items) != 1 {
		t.Fatalf("SelectItems() = %#v, %v", items, err)
	}
	if _, err := reader.MailboxOutbox().LoadEnvelope(ctx, "env-unified"); err != nil {
		t.Fatalf("LoadEnvelope() error = %v", err)
	}
	if _, err := reader.UserMessages().LoadMessage(ctx, run.ID, "msg-unified"); err != nil {
		t.Fatalf("LoadMessage() error = %v", err)
	}
	if spans, err := reader.Trace().ListTraceSpans(ctx, run.ID); err != nil || len(spans) != 1 {
		t.Fatalf("ListTraceSpans() = %#v, %v", spans, err)
	}
	if _, err := reader.Leases().LoadLease(ctx, lease.ID); err != nil {
		t.Fatalf("LoadLease() error = %v", err)
	}
	if _, err := reader.Approvals().LoadApproval(ctx, approval.ApprovalID); err != nil {
		t.Fatalf("LoadApproval() error = %v", err)
	}
	if _, err := reader.ResumeTokens().LoadResumeToken(ctx, token.TokenID); err != nil {
		t.Fatalf("LoadResumeToken() error = %v", err)
	}
	if _, err := reader.ActionAttempts().LoadActionAttempt(ctx, attempt.AttemptID); err != nil {
		t.Fatalf("LoadActionAttempt() error = %v", err)
	}
}

func TestMemoryProvider_CapabilityReporter(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	caps, err := provider.Capabilities(ctx)
	if err != nil {
		t.Fatalf("Capabilities() error = %v", err)
	}
	if !caps.SupportsTransactions {
		t.Fatalf("expected SupportsTransactions=true")
	}
	if !caps.SupportsBlackboardSubscribe {
		t.Fatalf("expected SupportsBlackboardSubscribe=true")
	}
	if !caps.SupportsListPending {
		t.Fatalf("expected SupportsListPending=true")
	}
	if caps.SupportsConcurrentWriters {
		t.Fatalf("expected SupportsConcurrentWriters=false (single-writer via gate)")
	}
}

func TestMemoryProvider_Close(t *testing.T) {
	provider := NewProvider()
	if err := provider.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestMemoryLeaseStore_AcquireWithExpectedVersion(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	cas, ok := uow.Leases().(interface {
		AcquireWithExpectedVersion(context.Context, model.TaskExecutionLease, uint64) (bool, error)
	})
	if !ok {
		t.Fatalf("leaseStore does not satisfy LeaseCAS")
	}

	lease := model.TaskExecutionLease{
		ID:         "lease-cas-1",
		RunID:      "run-1",
		TaskID:     "task-1",
		HolderID:   "worker-A",
		HolderType: model.HolderAgent,
		Status:     model.LeaseStatusActive,
		Expiry:     time.Now().Add(time.Minute),
	}
	acquired, err := cas.AcquireWithExpectedVersion(ctx, lease, 0)
	if err != nil {
		t.Fatalf("Acquire(version=0) error = %v", err)
	}
	if !acquired {
		t.Fatalf("expected first Acquire to succeed")
	}

	stale, err := cas.AcquireWithExpectedVersion(ctx, lease, 0)
	if err != nil {
		t.Fatalf("Acquire(stale) error = %v", err)
	}
	if stale {
		t.Fatalf("expected stale Acquire to return false")
	}

	active, err := cas.AcquireWithExpectedVersion(ctx, lease, 1)
	if err != nil {
		t.Fatalf("Acquire(active version=1) error = %v", err)
	}
	if active {
		t.Fatalf("expected an unexpired active lease to reject takeover")
	}

	loaded, err := uow.Leases().LoadLease(ctx, lease.ID)
	if err != nil {
		t.Fatalf("LoadLease() error = %v", err)
	}
	loaded.Status = model.LeaseStatusReleased
	if err := uow.Leases().SaveLease(ctx, loaded); err != nil {
		t.Fatalf("SaveLease(released) error = %v", err)
	}
	replacement := lease
	replacement.ID = "lease-cas-2"
	replacement.HolderID = "worker-B"
	winning, err := cas.AcquireWithExpectedVersion(ctx, replacement, 2)
	if err != nil {
		t.Fatalf("Acquire(replacement version=2) error = %v", err)
	}
	if !winning {
		t.Fatalf("expected released lease takeover to succeed")
	}
}

func TestMemoryLeaseStore_ExtendLease(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	cas := uow.Leases().(interface {
		AcquireWithExpectedVersion(context.Context, model.TaskExecutionLease, uint64) (bool, error)
		ExtendLease(context.Context, string, string, time.Time) (bool, error)
	})

	original := time.Now().Add(30 * time.Second)
	lease := model.TaskExecutionLease{
		ID:         "lease-ext-1",
		RunID:      "run-1",
		TaskID:     "task-1",
		HolderID:   "worker-A",
		HolderType: model.HolderAgent,
		Status:     model.LeaseStatusActive,
		Expiry:     original,
	}
	if _, err := cas.AcquireWithExpectedVersion(ctx, lease, 0); err != nil {
		t.Fatalf("Acquire error = %v", err)
	}

	newExpiry := original.Add(time.Minute)
	extended, err := cas.ExtendLease(ctx, lease.ID, "worker-A", newExpiry)
	if err != nil {
		t.Fatalf("ExtendLease(self) error = %v", err)
	}
	if !extended {
		t.Fatalf("expected ExtendLease(self) to succeed")
	}
	loaded, err := uow.Leases().LoadLease(ctx, lease.ID)
	if err != nil {
		t.Fatalf("LoadLease() error = %v", err)
	}
	if loaded.ExpiresAt.IsZero() || loaded.Expiry.IsZero() || !loaded.ExpiresAt.Equal(loaded.Expiry) || !loaded.ExpiresAt.Equal(newExpiry) {
		t.Fatalf("expected synchronized expiry fields after ExtendLease, got %+v", loaded)
	}

	rotated, err := cas.ExtendLease(ctx, lease.ID, "worker-B", newExpiry.Add(time.Minute))
	if err != nil {
		t.Fatalf("ExtendLease(other) error = %v", err)
	}
	if rotated {
		t.Fatalf("expected ExtendLease(other) to return false")
	}

	missing, err := cas.ExtendLease(ctx, "no-such-lease", "worker-A", newExpiry)
	if err != nil {
		t.Fatalf("ExtendLease(missing) error = %v", err)
	}
	if missing {
		t.Fatalf("expected ExtendLease(missing) to return false")
	}
}
