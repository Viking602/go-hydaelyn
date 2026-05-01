package core

import "context"

func (r *Runtime) Replay(runID string, mode ReplayMode) (Projection, error) {
	events, err := r.RunEvents(context.Background(), runID)
	if err != nil {
		return Projection{}, err
	}
	return replayProjection(events)
}

func (r *Runtime) ReplayRunState(runID string) (Projection, error) {
	return r.Replay(runID, ReplayModeAudit)
}

func (r *Runtime) Recover(_ context.Context, runID string) (Projection, error) {
	return r.Replay(runID, ReplayModeRecovery)
}
