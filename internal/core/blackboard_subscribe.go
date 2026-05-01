package core

import (
	"context"
	"errors"
	"slices"
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

// Subscribe streams future blackboard writes for runID that match filter. The
// caller MUST drain the channel and call cancel() when done; on full buffer
// (default 32) the runtime drops the oldest match (non-blocking writer).
func (r *Runtime) Subscribe(ctx context.Context, runID string, filter BlackboardFilter) (<-chan BlackboardItem, func() error, error) {
	if subscriber, ok := r.configuredBlackboardSubscriber(); ok {
		return subscriber.Subscribe(ctx, runID, filter)
	}
	return r.subscribeRuntimeHub(ctx, runID, filter)
}

func (r *Runtime) configuredBlackboardSubscriber() (BlackboardSubscriber, bool) {
	if r.storeProvider == nil {
		return nil, false
	}
	subscriber, ok := r.storeProvider.(BlackboardSubscriber)
	return subscriber, ok
}

func (r *Runtime) subscribeRuntimeHub(ctx context.Context, runID string, filter BlackboardFilter) (<-chan BlackboardItem, func() error, error) {
	selector := normalizeBlackboardSelector(filter)
	ch, cancel, err := r.memProvider.Subscribe(ctx, runID, selector)
	if err != nil {
		return nil, nil, err
	}
	wrapped := func() error {
		if err := cancel(); errors.Is(err, ErrNotFound) {
			return ErrSubscriptionClosed
		} else {
			return err
		}
	}
	return ch, wrapped, nil
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
