package hydaelyn

import (
	"context"
	"time"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/internal/core/adapter"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

func (r *Runner) WriteItem(ctx context.Context, item api.BlackboardItem) error {
	return adapter.ErrorToAPI(r.rt.WriteItem(ctx, adapter.BlackboardItemToModel(item)))
}

func (r *Runner) SelectItems(ctx context.Context, runID string, selector api.BlackboardSelector) ([]api.BlackboardItem, error) {
	items, err := r.rt.SelectItems(ctx, runID, adapter.BlackboardSelectorToModel(selector))
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return adapter.BlackboardItemsFromModel(items), nil
}

func (r *Runner) Subscribe(ctx context.Context, runID string, filter api.BlackboardFilter) (<-chan api.BlackboardItem, func() error, error) {
	items, cancel, err := r.rt.Subscribe(ctx, runID, adapter.BlackboardSelectorToModel(filter))
	if err != nil {
		return nil, nil, adapter.ErrorToAPI(err)
	}
	out := make(chan api.BlackboardItem)
	go func() {
		defer close(out)
		for item := range items {
			out <- adapter.BlackboardItemFromModel(item)
		}
	}()
	return out, func() error { return adapter.ErrorToAPI(cancel()) }, nil
}

func (r *Runner) WaitForBlackboard(ctx context.Context, runID string, filter api.BlackboardFilter, predicate func([]api.BlackboardItem) bool, timeout time.Duration) ([]api.BlackboardItem, error) {
	items, err := r.rt.WaitForBlackboard(ctx, runID, adapter.BlackboardSelectorToModel(filter), func(items []model.BlackboardItem) bool {
		return predicate(adapter.BlackboardItemsFromModel(items))
	}, timeout)
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return adapter.BlackboardItemsFromModel(items), nil
}
