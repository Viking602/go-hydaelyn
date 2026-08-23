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
	runner := NewDevelopment(api.Config{StoreProvider: failingBeginAPIProvider{err: errBoom, supportsListPending: true}})

	if tasks, err := runner.ReadyTasksContext(ctx, "run-1"); tasks != nil || !errors.Is(err, errBoom) {
		t.Fatalf("ReadyTasksContext() = %#v, %v, want nil, %v", tasks, err, errBoom)
	}
	if n, err := runner.ActiveLeaseCountContext(ctx, "run-1", "task-1"); n != 0 || !errors.Is(err, errBoom) {
		t.Fatalf("ActiveLeaseCountContext() = %d, %v, want 0, %v", n, err, errBoom)
	}
	if messages, err := runner.ResponseOutboxContext(ctx, "run-1"); messages != nil || !errors.Is(err, errBoom) {
		t.Fatalf("ResponseOutboxContext() = %#v, %v, want nil, %v", messages, err, errBoom)
	}
	if tokens, err := runner.ResumeTokensContext(ctx); tokens != nil || !errors.Is(err, errBoom) {
		t.Fatalf("ResumeTokensContext() = %#v, %v, want nil, %v", tokens, err, errBoom)
	}
}

func TestResumeTokensRejectsStoreWithoutListPending(t *testing.T) {
	runner := NewDevelopment(api.Config{StoreProvider: failingBeginAPIProvider{
		err:                 errors.New("begin should not run"),
		supportsListPending: false,
	}})
	if tokens, err := runner.ResumeTokensContext(context.Background()); tokens != nil || !errors.Is(err, api.ErrInvalidConfiguration) {
		t.Fatalf("ResumeTokensContext() = %#v, %v, want nil, %v", tokens, err, api.ErrInvalidConfiguration)
	}
	if tokens, err := runner.PendingResumeTokens(context.Background(), api.ResumeTokenSelector{}); tokens != nil || !errors.Is(err, api.ErrInvalidConfiguration) {
		t.Fatalf("PendingResumeTokens() = %#v, %v, want nil, %v", tokens, err, api.ErrInvalidConfiguration)
	}
}

type failingBeginAPIProvider struct {
	err                 error
	supportsListPending bool
}

func (p failingBeginAPIProvider) Begin(context.Context) (api.UnitOfWork, error) {
	return nil, p.err
}

func (p failingBeginAPIProvider) Capabilities(context.Context) (api.StoreCapabilities, error) {
	caps := api.DefaultStoreCapabilities()
	caps.SupportsListPending = p.supportsListPending
	return caps, nil
}
