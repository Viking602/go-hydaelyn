package memory

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/Viking602/venat/api"
)

type subscription struct {
	id     uint64
	runID  string
	filter api.BlackboardSelector
	ch     chan api.BlackboardItem
	done   <-chan struct{}
	once   sync.Once
}

type subscriptionHub struct {
	mu      sync.Mutex
	nextID  uint64
	dropped uint64
	subs    map[string][]*subscription
}

func newSubscriptionHub() *subscriptionHub {
	return &subscriptionHub{subs: map[string][]*subscription{}}
}

func (h *subscriptionHub) Subscribe(ctx context.Context, runID string, filter api.BlackboardSelector) (<-chan api.BlackboardItem, func() error, error) {
	if runID == "" {
		return nil, nil, api.ErrInvalidCommand
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	sub := &subscription{
		id:     h.nextID,
		runID:  runID,
		filter: filter,
		ch:     make(chan api.BlackboardItem, 32),
		done:   ctx.Done(),
	}
	h.subs[runID] = append(h.subs[runID], sub)
	stop := context.AfterFunc(ctx, func() { _ = h.unsubscribe(sub) })
	var stopped atomic.Bool
	return sub.ch, func() error {
		stop()
		err := h.unsubscribe(sub)
		if stopped.CompareAndSwap(false, true) {
			if errors.Is(err, api.ErrNotFound) {
				return nil
			}
			return err
		}
		return err
	}, nil
}

// DroppedCount is the number of fan-out items discarded because a
// subscriber buffer was full.
func (h *subscriptionHub) DroppedCount() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.dropped
}

func (h *subscriptionHub) unsubscribe(sub *subscription) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	list := h.subs[sub.runID]
	idx := slices.IndexFunc(list, func(current *subscription) bool { return current.id == sub.id })
	if idx < 0 {
		return api.ErrNotFound
	}
	h.subs[sub.runID] = slices.Delete(list, idx, idx+1)
	sub.once.Do(func() { close(sub.ch) })
	return nil
}

func (h *subscriptionHub) Notify(items []api.BlackboardItem) {
	if len(items) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, item := range items {
		for _, sub := range h.subs[item.RunID] {
			if !matchesBlackboardSelector(item, sub.filter) {
				continue
			}
			if sub.done != nil {
				select {
				case <-sub.done:
					continue
				default:
				}
			}
			select {
			case sub.ch <- item:
			default:
				select {
				case <-sub.ch:
					h.dropped++
				default:
				}
				select {
				case sub.ch <- item:
				default:
					h.dropped++
				}
			}
		}
	}
}

func selectBlackboardItems(state *State, runID string, selector api.BlackboardSelector) []api.BlackboardItem {
	items := make([]api.BlackboardItem, 0, len(state.Blackboard[runID]))
	for _, item := range state.Blackboard[runID] {
		if matchesBlackboardSelector(item, selector) {
			items = append(items, item)
			if selector.Limit > 0 && len(items) >= selector.Limit {
				break
			}
		}
	}
	return items
}

func matchesBlackboardSelector(item api.BlackboardItem, selector api.BlackboardSelector) bool {
	if selector.RunID != "" && item.RunID != selector.RunID {
		return false
	}
	if selector.TaskID != "" && item.TaskID != selector.TaskID {
		return false
	}
	if selector.Visibility != "" && item.Visibility != selector.Visibility {
		return false
	}
	if len(selector.ItemTypes) > 0 && !slices.Contains(selector.ItemTypes, item.Type) {
		return false
	}
	if len(selector.SourceIDs) > 0 && !slices.Contains(selector.SourceIDs, item.Source.ID) {
		return false
	}
	if len(selector.SourceTypes) > 0 && !slices.Contains(selector.SourceTypes, item.Source.Type) {
		return false
	}
	if selector.SinceVersion > 0 && item.Version <= selector.SinceVersion {
		return false
	}
	if len(selector.Keys) > 0 && !slices.Contains(selector.Keys, item.Key) {
		return false
	}
	return true
}
