package venat

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/venat/api"
)

func TestPublicReadAPIsFailClosedOnStoreBegin(t *testing.T) {
	ctx := context.Background()
	errBoom := errors.New("store begin failed")
	runner := NewDevelopment(api.Config{StoreProvider: failingBeginAPIProvider{err: errBoom}})

	if tasks, err := runner.ReadyTasksContext(ctx, "run-1"); tasks != nil || !errors.Is(err, errBoom) {
		t.Fatalf("ReadyTasksContext() = %#v, %v, want nil, %v", tasks, err, errBoom)
	}
	if tasks, err := runner.ReadyTasks("run-1"); tasks != nil || !errors.Is(err, errBoom) {
		t.Fatalf("ReadyTasks() = %#v, %v, want nil, %v", tasks, err, errBoom)
	}
	if n, err := runner.ActiveLeaseCountContext(ctx, "run-1", "task-1"); n != 0 || !errors.Is(err, errBoom) {
		t.Fatalf("ActiveLeaseCountContext() = %d, %v, want 0, %v", n, err, errBoom)
	}
	if n, err := runner.ActiveLeaseCount("run-1", "task-1"); n != 0 || !errors.Is(err, errBoom) {
		t.Fatalf("ActiveLeaseCount() = %d, %v, want 0, %v", n, err, errBoom)
	}
	if messages, err := runner.ResponseOutboxContext(ctx, "run-1"); messages != nil || !errors.Is(err, errBoom) {
		t.Fatalf("ResponseOutboxContext() = %#v, %v, want nil, %v", messages, err, errBoom)
	}
	if messages, err := runner.ResponseOutbox("run-1"); messages != nil || !errors.Is(err, errBoom) {
		t.Fatalf("ResponseOutbox() = %#v, %v, want nil, %v", messages, err, errBoom)
	}
	if tokens, err := runner.ResumeTokensContext(ctx); tokens != nil || !errors.Is(err, errBoom) {
		t.Fatalf("ResumeTokensContext() = %#v, %v, want nil, %v", tokens, err, errBoom)
	}
	if tokens, err := runner.ResumeTokens(); tokens != nil || !errors.Is(err, errBoom) {
		t.Fatalf("ResumeTokens() = %#v, %v, want nil, %v", tokens, err, errBoom)
	}
}

type failingBeginAPIProvider struct {
	err error
}

func (p failingBeginAPIProvider) Begin(context.Context) (api.UnitOfWork, error) {
	return nil, p.err
}
