package core

import (
	"context"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

func (r *Runtime) Replay(runID string, _ model.ReplayMode) (model.Projection, error) {
	events, err := r.RunEvents(context.Background(), runID)
	if err != nil {
		return model.Projection{}, err
	}
	return replayProjection(events)
}

func (r *Runtime) ReplayRunState(runID string) (model.Projection, error) {
	return r.Replay(runID, model.ReplayModeAudit)
}

func (r *Runtime) Recover(_ context.Context, runID string) (model.Projection, error) {
	return r.Replay(runID, model.ReplayModeRecovery)
}
