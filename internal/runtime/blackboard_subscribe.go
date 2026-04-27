package runtime

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// ErrSubscriptionClosed is returned by a subscription's cancel func if the
// caller cancels twice.
var ErrSubscriptionClosed = errors.New("orchestrator: blackboard subscription already closed")

// ErrWaitTimeout is returned by WaitForBlackboard when the timeout elapses
// before the predicate is satisfied.
var ErrWaitTimeout = errors.New("orchestrator: blackboard wait timed out")

// BlackboardFilter is the subset of BlackboardSelector used to match new items
// for streaming subscribers. RunID is implicit (set when subscribing).
type BlackboardFilter = BlackboardSelector

// blackboardSubscription is the runtime-internal record. The receiver-side
// channel is exposed via Subscribe.
type blackboardSubscription struct {
	id     uint64
	runID  string
	filter BlackboardFilter
	ch     chan BlackboardItem
	once   sync.Once
}

// Subscribe streams future blackboard writes for runID that match filter. The
// caller MUST drain the channel and call cancel() when done; on full buffer
// (default 32) the runtime drops the oldest match (non-blocking writer).
func (r *Runtime) Subscribe(_ context.Context, runID string, filter BlackboardFilter) (<-chan BlackboardItem, func() error, error) {
	if runID == "" {
		return nil, nil, fmt.Errorf("%w: subscribe requires runId", ErrInvalidCommand)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextSubID++
	sub := &blackboardSubscription{
		id:     r.nextSubID,
		runID:  runID,
		filter: normalizeBlackboardSelector(filter),
		ch:     make(chan BlackboardItem, 32),
	}
	r.subscribers[runID] = append(r.subscribers[runID], sub)
	cancel := func() error {
		return r.unsubscribe(sub)
	}
	return sub.ch, cancel, nil
}

func (r *Runtime) unsubscribe(sub *blackboardSubscription) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.subscribers[sub.runID]
	idx := slices.IndexFunc(list, func(s *blackboardSubscription) bool { return s.id == sub.id })
	if idx < 0 {
		return ErrSubscriptionClosed
	}
	r.subscribers[sub.runID] = slices.Delete(list, idx, idx+1)
	sub.once.Do(func() { close(sub.ch) })
	return nil
}

// notifySubscribersLocked fans out a freshly written item to matching
// subscribers under the runtime lock. Channel sends are non-blocking; if a
// subscriber's buffer is full, the oldest pending item is discarded to make
// room for the newest (subscribers are expected to keep up).
func (r *Runtime) notifySubscribersLocked(item BlackboardItem) {
	for _, sub := range r.subscribers[item.RunID] {
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

// WaitForBlackboard blocks until predicate returns true for the accumulated
// matching items, or the context/timeout expires. It first replays existing
// items that match filter, then streams new writes via Subscribe. The returned
// slice is the full set of matches observed when predicate first returned true.
//
// timeout=0 means "no timeout" — only ctx.Done() will end the wait.
func (r *Runtime) WaitForBlackboard(
	ctx context.Context,
	runID string,
	filter BlackboardFilter,
	predicate func([]BlackboardItem) bool,
	timeout time.Duration,
) ([]BlackboardItem, error) {
	if predicate == nil {
		return nil, fmt.Errorf("%w: WaitForBlackboard requires predicate", ErrInvalidCommand)
	}
	existing, err := r.SelectItems(ctx, runID, filter)
	if err != nil {
		return nil, err
	}
	if predicate(existing) {
		return existing, nil
	}
	ch, cancel, err := r.Subscribe(ctx, runID, filter)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cancel() }()

	var deadline <-chan time.Time
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		deadline = t.C
	}
	acc := append([]BlackboardItem{}, existing...)
	for {
		select {
		case <-ctx.Done():
			return acc, ctx.Err()
		case <-deadline:
			return acc, ErrWaitTimeout
		case item, ok := <-ch:
			if !ok {
				return acc, ErrSubscriptionClosed
			}
			acc = append(acc, item)
			if predicate(acc) {
				return acc, nil
			}
		}
	}
}

// matchesBlackboardSelector mirrors the filter logic used in SelectItems so
// streamed items match historical reads exactly.
func matchesBlackboardSelector(item BlackboardItem, selector BlackboardSelector) bool {
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
	if len(selector.SourceAgentIDs) > 0 && (item.Source.Type != SourceAgent || !slices.Contains(selector.SourceAgentIDs, item.Source.ID)) {
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
