package core

import (
	"context"

	"github.com/Viking602/go-hydaelyn/internal/core/memory"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func (r *Runtime) StoreProvider() StoreProvider {
	if r.storeProvider != nil {
		return r.storeProvider
	}
	return memStoreProvider{runtime: r}
}

func (r *Runtime) Begin(ctx context.Context) (UnitOfWork, error) {
	if r.storeProvider != nil {
		return r.storeProvider.Begin(ctx)
	}
	uow, err := r.memProvider.BeginFull(ctx)
	if err != nil {
		return nil, err
	}
	return memUnitOfWorkAdapter{full: uow, provider: r.memProvider}, nil
}

// WriteItem is the public BlackboardStore API. It goes through the UoW command
// path so policy, trace, and events are all recorded.
func (r *Runtime) WriteItem(ctx context.Context, item BlackboardItem) error {
	_, err := r.ExecuteCommand(ctx, WriteBlackboardItemCommand{Item: item})
	return err
}

// SelectItems is the public BlackboardStore API backed by memProvider.
func (r *Runtime) SelectItems(ctx context.Context, runID string, selector BlackboardSelector) ([]BlackboardItem, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	decision, err := r.currentPolicyEngine().Authorize(ctx, PolicyRequest{Operation: PolicyOperationBlackboardRead, RunID: runID, Selector: &selector})
	if err != nil {
		return nil, err
	}
	if decision.Effect == PolicyEffectDeny || decision.Effect == PolicyEffectAbort || decision.Effect == PolicyEffectRequireApproval || decision.Effect == PolicyEffectPause {
		return nil, ErrPolicyDenied
	}
	return uow.Blackboard().SelectItems(ctx, runID, selector)
}


// memStoreProvider adapts memProvider to the legacy StoreProvider interface.
type memStoreProvider struct {
	runtime *Runtime
}

func (p memStoreProvider) Begin(ctx context.Context) (UnitOfWork, error) {
	return p.runtime.Begin(ctx)
}

// memUnitOfWorkAdapter adapts memory.UnitOfWork (ports.FullUnitOfWork) to the
// legacy UnitOfWork interface, which requires Blackboard() BlackboardStore.
// BlackboardStore includes Subscribe(), so we wire that to the Provider hub.
type memUnitOfWorkAdapter struct {
	full     ports.FullUnitOfWork
	provider *memory.Provider
}

func (a memUnitOfWorkAdapter) Runs() RunStore     { return a.full.Runs() }
func (a memUnitOfWorkAdapter) Tasks() TaskStore   { return a.full.Tasks() }
func (a memUnitOfWorkAdapter) Events() EventStore { return a.full.Events() }
func (a memUnitOfWorkAdapter) Blackboard() BlackboardStore {
	return memBlackboardStoreAdapter{rw: a.full.Blackboard(), provider: a.provider}
}
func (a memUnitOfWorkAdapter) MailboxOutbox() MailboxOutboxStore { return a.full.MailboxOutbox() }
func (a memUnitOfWorkAdapter) UserMessages() UserMessageStore    { return a.full.UserMessages() }
func (a memUnitOfWorkAdapter) Trace() TraceStore                 { return a.full.Trace() }
func (a memUnitOfWorkAdapter) Commit(ctx context.Context) error  { return a.full.Commit(ctx) }
func (a memUnitOfWorkAdapter) Rollback(ctx context.Context) error { return a.full.Rollback(ctx) }

// memBlackboardStoreAdapter adds Subscribe() on top of ports.BlackboardReadWriter.
type memBlackboardStoreAdapter struct {
	rw       ports.BlackboardReadWriter
	provider *memory.Provider
}

func (a memBlackboardStoreAdapter) WriteItem(ctx context.Context, item BlackboardItem) error {
	return a.rw.WriteItem(ctx, item)
}

func (a memBlackboardStoreAdapter) SelectItems(ctx context.Context, runID string, selector BlackboardSelector) ([]BlackboardItem, error) {
	return a.rw.SelectItems(ctx, runID, selector)
}

func (a memBlackboardStoreAdapter) Subscribe(ctx context.Context, runID string, filter BlackboardFilter) (<-chan BlackboardItem, func() error, error) {
	return a.provider.Subscribe(ctx, runID, filter)
}
