package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func TestMemoryUnitOfWorkRollbackRunStore(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	uow, err := provider.BeginFull(ctx)
	if err != nil {
		t.Fatalf("BeginFull() error = %v", err)
	}
	if err := uow.Runs().SaveRun(ctx, model.Run{ID: "run-1", Status: model.RunStatusCreated}); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	if err := uow.Rollback(ctx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	reader, err := provider.BeginFull(ctx)
	if err != nil {
		t.Fatalf("BeginFull() reader error = %v", err)
	}
	defer func() { _ = reader.Rollback(ctx) }()
	if _, err := reader.Runs().LoadRun(ctx, "run-1"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("LoadRun() after rollback error = %v, want ErrNotFound", err)
	}
}

func TestMemoryUnitOfWorkCommitRunStore(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	uow, err := provider.BeginFull(ctx)
	if err != nil {
		t.Fatalf("BeginFull() error = %v", err)
	}
	if err := uow.Runs().SaveRun(ctx, model.Run{ID: "run-1", Status: model.RunStatusCreated}); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	if err := uow.Commit(ctx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	reader, err := provider.BeginFull(ctx)
	if err != nil {
		t.Fatalf("BeginFull() reader error = %v", err)
	}
	defer func() { _ = reader.Rollback(ctx) }()
	if _, err := reader.Runs().LoadRun(ctx, "run-1"); err != nil {
		t.Fatalf("LoadRun() after commit error = %v", err)
	}
}

func TestMemoryUnitOfWorkRollbackLeaseStore(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	uow, err := provider.BeginFull(ctx)
	if err != nil {
		t.Fatalf("BeginFull() error = %v", err)
	}
	lease := model.TaskExecutionLease{ID: "lease-1", RunID: "run-1", TaskID: "task-1", Status: model.LeaseStatusActive}
	if err := uow.Leases().SaveLease(ctx, lease); err != nil {
		t.Fatalf("SaveLease() error = %v", err)
	}
	if err := uow.Rollback(ctx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	reader, err := provider.BeginFull(ctx)
	if err != nil {
		t.Fatalf("BeginFull() reader error = %v", err)
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
	uow, err := provider.BeginFull(ctx)
	if err != nil {
		t.Fatalf("BeginFull() error = %v", err)
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
	uow, err := provider.BeginFull(ctx)
	if err != nil {
		t.Fatalf("BeginFull() error = %v", err)
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
	uow, err := provider.BeginFull(ctx)
	if err != nil {
		t.Fatalf("BeginFull() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	_, err = provider.BeginFull(waitCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("BeginFull(waitCtx) error = %v, want DeadlineExceeded", err)
	}
}

func TestMemoryProviderCommittedReadDoesNotWaitForActiveWriteTransaction(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	uow, err := provider.BeginFull(ctx)
	if err != nil {
		t.Fatalf("BeginFull() error = %v", err)
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

func TestFallbackLeaseRollbackDoesNotLeaveGhostLease(t *testing.T) {
	ctx := context.Background()
	provider := NewFallbackProvider()
	tx, err := provider.BeginFallback(ctx, ports.MissingOptionalStores{})
	if err != nil {
		t.Fatalf("BeginFallback() error = %v", err)
	}
	lease := model.TaskExecutionLease{ID: "lease-1", RunID: "run-1", TaskID: "task-1", Status: model.LeaseStatusActive}
	if err := tx.Leases().SaveLease(ctx, lease); err != nil {
		t.Fatalf("SaveLease() error = %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if _, ok := provider.Snapshot().Leases["lease-1"]; ok {
		t.Fatal("fallback rollback left ghost lease")
	}
}
