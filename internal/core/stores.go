package core

import "context"

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
