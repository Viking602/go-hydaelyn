package core

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/ports"
	"github.com/Viking602/venat/internal/memory"
)

func TestReadAPIsFailClosedOnStoreBegin(t *testing.T) {
	ctx := context.Background()
	errBoom := errors.New("store begin failed")
	rt := NewRuntime(Config{StoreProvider: failingBeginStoreProvider{err: errBoom, supportsListPending: true}})

	if tasks, err := rt.ReadyTasks(ctx, "run-1"); tasks != nil || !errors.Is(err, errBoom) {
		t.Fatalf("ReadyTasks() = %#v, %v, want nil, %v", tasks, err, errBoom)
	}
	if n, err := rt.ActiveLeaseCount(ctx, "run-1", "task-1"); n != 0 || !errors.Is(err, errBoom) {
		t.Fatalf("ActiveLeaseCount() = %d, %v, want 0, %v", n, err, errBoom)
	}
	if messages, err := rt.ResponseOutbox(ctx, "run-1"); messages != nil || !errors.Is(err, errBoom) {
		t.Fatalf("ResponseOutbox() = %#v, %v, want nil, %v", messages, err, errBoom)
	}
	if tokens, err := rt.ResumeTokens(ctx); tokens != nil || !errors.Is(err, errBoom) {
		t.Fatalf("ResumeTokens() = %#v, %v, want nil, %v", tokens, err, errBoom)
	}
}

func TestResumeTokensRejectsStoreWithoutListPending(t *testing.T) {
	ctx := context.Background()
	rt := NewRuntime(Config{StoreProvider: failingBeginStoreProvider{
		err:                 errors.New("begin should not run"),
		supportsListPending: false,
	}})
	if tokens, err := rt.ResumeTokens(ctx); tokens != nil || !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("ResumeTokens() = %#v, %v, want nil, %v", tokens, err, ErrInvalidConfiguration)
	}
	if tokens, err := rt.PendingResumeTokens(ctx, api.ResumeTokenSelector{}); tokens != nil || !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("PendingResumeTokens() = %#v, %v, want nil, %v", tokens, err, ErrInvalidConfiguration)
	}
}

func TestReadAPIsFailClosedOnReadCleanup(t *testing.T) {
	ctx := context.Background()
	errBoom := errors.New("read rollback failed")
	rt := NewRuntime(Config{StoreProvider: failingRollbackStoreProvider{
		inner: memory.NewProvider(),
		err:   errBoom,
	}})

	if _, err := rt.ReadyTasks(ctx, "run-1"); !errors.Is(err, errBoom) {
		t.Fatalf("ReadyTasks() error = %v, want %v", err, errBoom)
	}
	if _, err := rt.ActiveLeaseCount(ctx, "run-1", "task-1"); !errors.Is(err, errBoom) {
		t.Fatalf("ActiveLeaseCount() error = %v, want %v", err, errBoom)
	}
	if _, err := rt.ResponseOutbox(ctx, "run-1"); !errors.Is(err, errBoom) {
		t.Fatalf("ResponseOutbox() error = %v, want %v", err, errBoom)
	}
	if _, err := rt.ResumeTokens(ctx); !errors.Is(err, errBoom) {
		t.Fatalf("ResumeTokens() error = %v, want %v", err, errBoom)
	}
}

func TestReadAPIsFailClosedOnStoreRead(t *testing.T) {
	ctx := context.Background()
	errBoom := errors.New("store read failed")
	rt := NewRuntime(Config{StoreProvider: failingReadStoreProvider{
		inner: memory.NewProvider(),
		err:   errBoom,
	}})

	if tasks, err := rt.ReadyTasks(ctx, "run-1"); tasks != nil || !errors.Is(err, errBoom) {
		t.Fatalf("ReadyTasks() = %#v, %v, want nil, %v", tasks, err, errBoom)
	}
	if n, err := rt.ActiveLeaseCount(ctx, "run-1", "task-1"); n != 0 || !errors.Is(err, errBoom) {
		t.Fatalf("ActiveLeaseCount() = %d, %v, want 0, %v", n, err, errBoom)
	}
	if messages, err := rt.ResponseOutbox(ctx, "run-1"); messages != nil || !errors.Is(err, errBoom) {
		t.Fatalf("ResponseOutbox() = %#v, %v, want nil, %v", messages, err, errBoom)
	}
	if tokens, err := rt.ResumeTokens(ctx); tokens != nil || !errors.Is(err, errBoom) {
		t.Fatalf("ResumeTokens() = %#v, %v, want nil, %v", tokens, err, errBoom)
	}
}

type failingBeginStoreProvider struct {
	err                 error
	supportsListPending bool
}

func (p failingBeginStoreProvider) Begin(context.Context) (UnitOfWork, error) {
	return nil, p.err
}

func (p failingBeginStoreProvider) Capabilities(context.Context) (ports.StoreCapabilities, error) {
	return ports.StoreCapabilities{SupportsListPending: p.supportsListPending}, nil
}

type failingReadStoreProvider struct {
	inner StoreProvider
	err   error
}

func (p failingReadStoreProvider) Capabilities(ctx context.Context) (ports.StoreCapabilities, error) {
	return p.inner.(CapabilityReporter).Capabilities(ctx)
}

func (p failingReadStoreProvider) Begin(ctx context.Context) (UnitOfWork, error) {
	uow, err := p.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return failingReadUnitOfWork{UnitOfWork: uow, err: p.err}, nil
}

type failingReadUnitOfWork struct {
	UnitOfWork
	err error
}

func (u failingReadUnitOfWork) Tasks() TaskStore {
	return failingTaskStore{TaskStore: u.UnitOfWork.Tasks(), err: u.err}
}

func (u failingReadUnitOfWork) Leases() LeaseStore {
	return failingLeaseStore{LeaseStore: u.UnitOfWork.Leases(), err: u.err}
}

func (u failingReadUnitOfWork) UserMessages() UserMessageStore {
	return failingUserMessageStore{UserMessageStore: u.UnitOfWork.UserMessages(), err: u.err}
}

func (u failingReadUnitOfWork) ResumeTokens() ResumeTokenStore {
	return failingResumeTokenStore{ResumeTokenStore: u.UnitOfWork.ResumeTokens(), err: u.err}
}

type failingTaskStore struct {
	TaskStore
	err error
}

func (s failingTaskStore) ListTasks(context.Context, string) ([]Task, error) {
	return nil, s.err
}

type failingLeaseStore struct {
	LeaseStore
	err error
}

func (s failingLeaseStore) ActiveLeaseForTask(context.Context, string, string) (TaskExecutionLease, bool, error) {
	return TaskExecutionLease{}, false, s.err
}

type failingUserMessageStore struct {
	UserMessageStore
	err error
}

func (s failingUserMessageStore) ListMessages(context.Context, string) ([]UserMessage, error) {
	return nil, s.err
}

type failingResumeTokenStore struct {
	ResumeTokenStore
	err error
}

func (s failingResumeTokenStore) ListPending(context.Context, api.ResumeTokenSelector) ([]ResumeToken, error) {
	return nil, s.err
}

type failingRollbackStoreProvider struct {
	inner StoreProvider
	err   error
}

func (p failingRollbackStoreProvider) Capabilities(ctx context.Context) (ports.StoreCapabilities, error) {
	return p.inner.(CapabilityReporter).Capabilities(ctx)
}

func (p failingRollbackStoreProvider) Begin(ctx context.Context) (UnitOfWork, error) {
	uow, err := p.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return failingRollbackUnitOfWork{UnitOfWork: uow, err: p.err}, nil
}

type failingRollbackUnitOfWork struct {
	UnitOfWork
	err error
}

func (u failingRollbackUnitOfWork) Rollback(ctx context.Context) error {
	_ = u.UnitOfWork.Rollback(ctx)
	return u.err
}
