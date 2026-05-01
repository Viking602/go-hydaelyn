package core

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/go-hydaelyn/internal/core/memory"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func TestTransactionRunnerDoesNotStartFallbackForFullMemoryUoW(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	fallback := &spyFallbackProvider{}
	uow, err := beginFullUoW(ctx, provider, fallback)
	if err != nil {
		t.Fatalf("beginFullUoW() error = %v", err)
	}
	if fallback.called != 0 {
		t.Fatalf("fallback BeginFallback called %d time(s), want 0", fallback.called)
	}
	if err := uow.Rollback(ctx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
}

func TestTransactionRunnerReturnsInvalidConfigurationWhenFallbackProviderNil(t *testing.T) {
	ctx := context.Background()
	base := &partialUOW{}
	_, err := beginFullUoW(ctx, partialProvider{uow: base}, nil)
	if !errors.Is(err, model.ErrInvalidConfiguration) {
		t.Fatalf("beginFullUoW() error = %v, want ErrInvalidConfiguration", err)
	}
	if base.rollbackCount != 1 {
		t.Fatalf("rollbackCount = %d, want 1", base.rollbackCount)
	}
}

type spyFallbackProvider struct {
	called int
}

func (s *spyFallbackProvider) BeginFallback(context.Context, ports.MissingOptionalStores) (ports.FallbackTx, error) {
	s.called++
	return nil, nil
}

type partialProvider struct {
	uow *partialUOW
}

func (p partialProvider) Begin(context.Context) (ports.UnitOfWork, error) {
	return p.uow, nil
}

type partialUOW struct {
	rollbackCount int
}

func (u *partialUOW) Runs() ports.RunStore                    { return nil }
func (u *partialUOW) Tasks() ports.TaskStore                  { return nil }
func (u *partialUOW) Events() ports.EventStore                { return nil }
func (u *partialUOW) Blackboard() ports.BlackboardReadWriter  { return nil }
func (u *partialUOW) MailboxOutbox() ports.MailboxOutboxStore { return nil }
func (u *partialUOW) UserMessages() ports.UserMessageStore    { return nil }
func (u *partialUOW) Trace() ports.TraceStore                 { return nil }
func (u *partialUOW) Commit(context.Context) error            { return nil }
func (u *partialUOW) Rollback(context.Context) error {
	u.rollbackCount++
	return nil
}
