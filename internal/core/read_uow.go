package core

import (
	"context"

	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func (r *Runtime) beginReadUoW(ctx context.Context) (ports.FullUnitOfWork, func(), error) {
	uow, err := r.beginWriteUoW(ctx)
	if err != nil {
		return nil, nil, err
	}
	return uow, func() { _ = uow.Rollback(ctx) }, nil
}

func (r *Runtime) beginWriteUoW(ctx context.Context) (ports.FullUnitOfWork, error) {
	if r.storeProvider == nil {
		return r.memProvider.BeginFull(ctx)
	}
	return beginFullUoW(ctx, legacyToPortsStoreProvider{provider: r.storeProvider}, r.fallbackProvider)
}

// legacyToPortsStoreProvider adapts the legacy StoreProvider (returns UnitOfWork)
// to ports.StoreProvider (returns ports.UnitOfWork).
type legacyToPortsStoreProvider struct {
	provider StoreProvider
}

func (a legacyToPortsStoreProvider) Begin(ctx context.Context) (ports.UnitOfWork, error) {
	uow, err := a.provider.Begin(ctx)
	if err != nil {
		return nil, err
	}
	// Wrap the legacy UnitOfWork (Blackboard() BlackboardStore) into ports.UnitOfWork
	// (Blackboard() ports.BlackboardReadWriter). BlackboardStore is a superset so the
	// concrete value is compatible; we just need the method signature to match.
	return legacyUoWToPortsAdapter{uow: uow}, nil
}

// legacyUoWToPortsAdapter adapts the legacy UnitOfWork (Blackboard() BlackboardStore)
// to ports.UnitOfWork (Blackboard() ports.BlackboardReadWriter).
type legacyUoWToPortsAdapter struct {
	uow UnitOfWork
}

func (a legacyUoWToPortsAdapter) Runs() ports.RunStore     { return a.uow.Runs() }
func (a legacyUoWToPortsAdapter) Tasks() ports.TaskStore   { return a.uow.Tasks() }
func (a legacyUoWToPortsAdapter) Events() ports.EventStore { return a.uow.Events() }
func (a legacyUoWToPortsAdapter) Blackboard() ports.BlackboardReadWriter {
	return a.uow.Blackboard()
}
func (a legacyUoWToPortsAdapter) MailboxOutbox() ports.MailboxOutboxStore {
	return a.uow.MailboxOutbox()
}
func (a legacyUoWToPortsAdapter) UserMessages() ports.UserMessageStore {
	return a.uow.UserMessages()
}
func (a legacyUoWToPortsAdapter) Trace() ports.TraceStore            { return a.uow.Trace() }
func (a legacyUoWToPortsAdapter) Commit(ctx context.Context) error   { return a.uow.Commit(ctx) }
func (a legacyUoWToPortsAdapter) Rollback(ctx context.Context) error { return a.uow.Rollback(ctx) }
