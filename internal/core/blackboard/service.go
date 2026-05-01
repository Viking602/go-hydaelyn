package blackboard

import (
	"context"
	"errors"
	"time"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

var ErrWaitTimeout = errors.New("orchestrator: blackboard wait timed out")

type Service struct {
	Reader     ports.BlackboardCommittedReader
	Subscriber ports.BlackboardSubscriber
}

func NewService(reader ports.BlackboardCommittedReader, subscriber ports.BlackboardSubscriber) *Service {
	return &Service{Reader: reader, Subscriber: subscriber}
}

func (s *Service) Subscribe(ctx context.Context, runID string, filter model.BlackboardSelector) (<-chan model.BlackboardItem, func() error, error) {
	if s == nil || s.Subscriber == nil {
		return nil, nil, model.ErrInvalidConfiguration
	}
	return s.Subscriber.Subscribe(ctx, runID, filter)
}

func (s *Service) WaitForBlackboard(ctx context.Context, runID string, filter model.BlackboardSelector, predicate func([]model.BlackboardItem) bool, timeout time.Duration) ([]model.BlackboardItem, error) {
	if predicate == nil {
		return nil, model.ErrInvalidCommand
	}
	if s == nil || s.Reader == nil || s.Subscriber == nil {
		return nil, model.ErrInvalidConfiguration
	}
	ch, cancel, err := s.Subscriber.Subscribe(ctx, runID, filter)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cancel() }()

	seen := map[string]struct{}{}
	acc := []model.BlackboardItem{}
	appendUnique := func(items ...model.BlackboardItem) {
		for _, item := range items {
			if item.ID != "" {
				if _, ok := seen[item.ID]; ok {
					continue
				}
				seen[item.ID] = struct{}{}
			}
			acc = append(acc, item)
		}
	}
	existing, err := s.Reader.SelectItems(ctx, runID, filter)
	if err != nil {
		return nil, err
	}
	appendUnique(existing...)
	if predicate(acc) {
		return acc, nil
	}

	var deadline <-chan time.Time
	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		deadline = timer.C
	}
	for {
		select {
		case <-ctx.Done():
			return acc, ctx.Err()
		case <-deadline:
			return acc, ErrWaitTimeout
		case item, ok := <-ch:
			if !ok {
				return acc, model.ErrNotFound
			}
			appendUnique(item)
			if predicate(acc) {
				return acc, nil
			}
		}
	}
}
