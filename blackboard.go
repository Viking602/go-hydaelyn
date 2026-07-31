package venat

import (
	"context"
	"sync"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/adapter"
	"github.com/Viking602/venat/internal/core/model"
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

// Subscribe streams future blackboard writes for runID that match filter.
// The subscription ends when the returned cancel func is called or when
// ctx is cancelled — either way the underlying subscription is released
// and the channel closes; items in flight at that moment may be dropped.
// cancel is idempotent and safe to call after ctx cancellation.
func (r *Runner) Subscribe(ctx context.Context, runID string, filter api.BlackboardFilter) (<-chan api.BlackboardItem, func() error, error) {
	items, cancel, err := r.rt.Subscribe(ctx, runID, adapter.BlackboardSelectorToModel(filter))
	if err != nil {
		return nil, nil, adapter.ErrorToAPI(err)
	}
	// stopFwd lets teardown unblock a forwarder parked on `out <-` after
	// the consumer stopped reading; closing the upstream channel alone
	// does not release a pending send.
	fwd, stopFwd := context.WithCancel(ctx)
	out := make(chan api.BlackboardItem)
	go func() {
		defer close(out)
		for item := range items {
			select {
			case out <- adapter.BlackboardItemFromModel(item):
			case <-fwd.Done():
				return
			}
		}
	}()
	// teardown runs at most once, shared between the returned cancel func
	// and ctx cancellation (the runtime hub ignores ctx, so without the
	// AfterFunc an abandoned ctx would leave the subscription registered).
	var once sync.Once
	var cancelErr error
	teardown := func() error {
		once.Do(func() {
			stopFwd()
			cancelErr = cancel()
		})
		return cancelErr
	}
	stopAfter := context.AfterFunc(ctx, func() { _ = teardown() })
	return out, func() error {
		stopAfter()
		return adapter.ErrorToAPI(teardown())
	}, nil
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
