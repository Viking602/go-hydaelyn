package core

import (
	"context"
	"fmt"
	"time"
)

// WaitForBlackboard blocks until predicate returns true for the accumulated
// matching items, or the context/timeout expires. It subscribes before replaying
// existing items so writes committed during the replay window are either present
// in the replay result or delivered by the stream. Items are de-duplicated by ID
// to avoid double-counting writes that appear in both paths.
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
	ch, cancel, err := r.Subscribe(ctx, runID, filter)
	if err != nil {
		return nil, err
	}

	existing, err := r.SelectItems(ctx, runID, filter)
	if err != nil {
		_ = cancel()
		return nil, err
	}
	defer func() { _ = cancel() }()

	var deadline <-chan time.Time
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		deadline = t.C
	}
	seen := map[string]struct{}{}
	acc := appendUniqueBlackboardItems(nil, seen, existing...)
	if predicate(acc) {
		return acc, nil
	}
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
			acc = appendUniqueBlackboardItems(acc, seen, item)
			if predicate(acc) {
				return acc, nil
			}
		}
	}
}

func appendUniqueBlackboardItems(acc []BlackboardItem, seen map[string]struct{}, items ...BlackboardItem) []BlackboardItem {
	for _, item := range items {
		if item.ID != "" {
			if _, ok := seen[item.ID]; ok {
				continue
			}
			seen[item.ID] = struct{}{}
		}
		acc = append(acc, item)
	}
	return acc
}
