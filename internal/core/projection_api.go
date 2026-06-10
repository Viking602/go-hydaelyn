package core

import (
	"context"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

func (r *Runtime) Replay(ctx context.Context, runID string, _ model.ReplayMode) (model.Projection, error) {
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
