package core

import (
	"context"

	"github.com/Viking602/venat/api"
)

func (r *Runtime) Replay(ctx context.Context, runID string, mode api.ReplayMode) (api.Projection, error) {
	switch mode {
	case api.ReplayModeAudit:
	case api.ReplayModeRecovery:
		if err := r.recoverExpiredTaskExecutions(ctx, runID); err != nil {
			return api.Projection{}, err
		}
	default:
		return api.Projection{}, api.ErrInvalidCommand
	}
	events, err := r.RunEvents(ctx, runID)
	if err != nil {
		return api.Projection{}, err
	}
	return replayProjection(events)
}

func (r *Runtime) ReplayRunState(ctx context.Context, runID string) (api.Projection, error) {
	return r.Replay(ctx, runID, api.ReplayModeAudit)
}

func (r *Runtime) Recover(ctx context.Context, runID string) (api.Projection, error) {
	return r.Replay(ctx, runID, api.ReplayModeRecovery)
}
