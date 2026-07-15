package core

import (
	"context"

	blackboardsvc "github.com/Viking602/go-hydaelyn/internal/blackboard"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func (r *Runtime) StoreProvider() StoreProvider {
	if r.storeProvider != nil {
		return r.storeProvider
	}
	return r.memProvider
}

func (r *Runtime) Begin(ctx context.Context) (UnitOfWork, error) {
	if r.storeProvider != nil {
		return r.storeProvider.Begin(ctx)
	}
	return r.memProvider.Begin(ctx)
}

func (r *Runtime) StoreCapabilities(ctx context.Context) (ports.StoreCapabilities, error) {
	reporter, ok := r.StoreProvider().(ports.CapabilityReporter)
	if !ok {
		return ports.DefaultStoreCapabilities(), nil
	}
	return reporter.Capabilities(ctx)
}

func (r *Runtime) Close(ctx context.Context) error {
	closer, ok := r.StoreProvider().(ports.ProviderCloser)
	if !ok {
		return nil
	}
	return closer.Close(ctx)
}

// WriteItem is the public BlackboardStore API. It goes through the UoW command
// path so policy, trace, and events are all recorded.
func (r *Runtime) WriteItem(ctx context.Context, item model.BlackboardItem) error {
	_, err := r.ExecuteCommand(ctx, WriteBlackboardItemCommand{Item: item})
	return err
}

func registerBlackboardUoWCommandHandlers(runtime *Runtime) {
	blackboardsvc.RegisterHandlers(runtime.commandBus, blackboardsvc.HandlerOptions{
		NewID:     runtime.newID,
		Authorize: runtime.authorizeUoW,
	})
}

// SelectItems is the public BlackboardStore read API backed by the configured store provider.
func (r *Runtime) SelectItems(ctx context.Context, runID string, selector model.BlackboardSelector) ([]model.BlackboardItem, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	decision, err := r.currentPolicyEngine().Authorize(ctx, model.PolicyRequest{Operation: model.PolicyOperationBlackboardRead, RunID: runID, Selector: &selector})
	if err != nil {
		return nil, err
	}
	if decision.Effect == model.PolicyEffectDeny || decision.Effect == model.PolicyEffectAbort || decision.Effect == model.PolicyEffectRequireApproval || decision.Effect == model.PolicyEffectPause {
		return nil, ErrPolicyDenied
	}
	return uow.Blackboard().SelectItems(ctx, runID, selector)
}
