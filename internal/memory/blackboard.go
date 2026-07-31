package memory

import (
	"context"
	"slices"
	"sync"

	"github.com/Viking602/venat/internal/core/model"
)

type subscription struct {
	id     uint64
	runID  string
	filter model.BlackboardSelector
	ch     chan model.BlackboardItem
	once   sync.Once
}

type subscriptionHub struct {
	mu     sync.Mutex
	nextID uint64
	subs   map[string][]*subscription
}

func newSubscriptionHub() *subscriptionHub {
	return &subscriptionHub{subs: map[string][]*subscription{}}
}

func (h *subscriptionHub) Subscribe(_ context.Context, runID string, filter model.BlackboardSelector) (<-chan model.BlackboardItem, func() error, error) {
	if runID == "" {
		return nil, nil, model.ErrInvalidCommand
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	sub := &subscription{
		id:     h.nextID,
		runID:  runID,
		filter: filter,
		ch:     make(chan model.BlackboardItem, 32),
	}
	h.subs[runID] = append(h.subs[runID], sub)
	return sub.ch, func() error { return h.unsubscribe(sub) }, nil
}

func (h *subscriptionHub) unsubscribe(sub *subscription) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	list := h.subs[sub.runID]
	idx := slices.IndexFunc(list, func(current *subscription) bool { return current.id == sub.id })
	if idx < 0 {
		return model.ErrNotFound
	}
	h.subs[sub.runID] = slices.Delete(list, idx, idx+1)
	sub.once.Do(func() { close(sub.ch) })
	return nil
}

func (h *subscriptionHub) Notify(items []model.BlackboardItem) {
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
			select {
			case sub.ch <- item:
			default:
				select {
				case <-sub.ch:
				default:
				}
				select {
				case sub.ch <- item:
				default:
				}
			}
		}
	}
}

func selectBlackboardItems(state *State, runID string, selector model.BlackboardSelector) []model.BlackboardItem {
	items := make([]model.BlackboardItem, 0, len(state.Blackboard[runID]))
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

func matchesBlackboardSelector(item model.BlackboardItem, selector model.BlackboardSelector) bool {
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
