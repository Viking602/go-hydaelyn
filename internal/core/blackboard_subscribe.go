package core

import (
	"context"
	"errors"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

// ErrSubscriptionClosed is returned by a subscription's cancel func if the
// caller cancels twice.
var ErrSubscriptionClosed = errors.New("orchestrator: blackboard subscription already closed")

// ErrWaitTimeout is returned by WaitForBlackboard when the timeout elapses
// before the predicate is satisfied.
var ErrWaitTimeout = errors.New("orchestrator: blackboard wait timed out")

// BlackboardFilter is the subset of BlackboardSelector used to match new items
// for streaming subscribers. RunID is implicit (set when subscribing).
type BlackboardFilter = model.BlackboardSelector

// Subscribe streams future blackboard writes for runID that match filter. The
// caller MUST drain the channel and call cancel() when done; on full buffer
// (default 32) the runtime drops the oldest match (non-blocking writer).
func (r *Runtime) Subscribe(ctx context.Context, runID string, filter BlackboardFilter) (<-chan model.BlackboardItem, func() error, error) {
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

func (r *Runtime) subscribeRuntimeHub(ctx context.Context, runID string, filter BlackboardFilter) (<-chan model.BlackboardItem, func() error, error) {
	ch, cancel, err := r.memProvider.Subscribe(ctx, runID, filter)
	if err != nil {
		return nil, nil, err
	}
	wrapped := func() error {
		err := cancel()
		if errors.Is(err, ErrNotFound) {
			return ErrSubscriptionClosed
		}
		return err
	}
	return ch, wrapped, nil
}
