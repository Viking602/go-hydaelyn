package core

import (
	"context"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

func (r *Runtime) Replay(ctx context.Context, runID string, mode model.ReplayMode) (model.Projection, error) {
	switch mode {
	case model.ReplayModeAudit:
	case model.ReplayModeRecovery:
		if err := r.recoverExpiredTaskExecutions(ctx, runID); err != nil {
			return model.Projection{}, err
		}
	default:
		return model.Projection{}, model.ErrInvalidCommand
	}
	events, err := r.RunEvents(ctx, runID)
	if err != nil {
		return model.Projection{}, err
	}
	return replayProjection(events)
}

func (r *Runtime) ReplayRunState(ctx context.Context, runID string) (model.Projection, error) {
	return r.Replay(ctx, runID, model.ReplayModeAudit)
}

func (r *Runtime) Recover(ctx context.Context, runID string) (model.Projection, error) {
	return r.Replay(ctx, runID, model.ReplayModeRecovery)
}
