package core

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/memory"
)

func TestReadAPIsFailClosedOnStoreBegin(t *testing.T) {
	ctx := context.Background()
	errBoom := errors.New("store begin failed")
	rt := NewRuntime(Config{StoreProvider: failingBeginStoreProvider{err: errBoom}})

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
	err error
}

func (p failingBeginStoreProvider) Begin(context.Context) (UnitOfWork, error) {
	return nil, p.err
}

type failingReadStoreProvider struct {
	inner StoreProvider
	err   error
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

func (s failingResumeTokenStore) ListPending(context.Context, model.ResumeTokenSelector) ([]ResumeToken, error) {
	return nil, s.err
}
