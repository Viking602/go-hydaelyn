package venat

import (
	"context"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core"
)

func (r *Runner) QueueRun(ctx context.Context, cmd api.StartRunCommand) (api.Run, error) {
	run, err := r.rt.QueueRun(ctx, cmd)
	if err != nil {
		return api.Run{}, err
	}
	return run, nil
}

// StartRunWithResult starts a run and reports whether this call created it.
// Created is false for an idempotent retry that returned the existing run.
func (r *Runner) StartRunWithResult(ctx context.Context, cmd api.StartRunCommand) (api.StartRunResult, error) {
	started, err := r.rt.StartRunWithResult(ctx, cmd)
	if err != nil {
		return api.StartRunResult{}, err
	}
	return api.StartRunResult{
		Run:      started.Run,
		RootTask: started.Root,
		Created:  started.Created,
	}, nil
}

func (r *Runner) StartRun(ctx context.Context, cmd api.StartRunCommand) (api.Run, api.Task, error) {
	started, err := r.StartRunWithResult(ctx, cmd)
	if err != nil {
		return api.Run{}, api.Task{}, err
	}
	return started.Run, started.RootTask, nil
}

func (r *Runner) AdvanceRun(ctx context.Context, cmd api.AdvanceRunCommand) (api.Run, error) {
	run, err := r.rt.AdvanceRun(ctx, core.AdvanceRunCommand(cmd))
	if err != nil {
		return api.Run{}, err
	}
	return run, nil
}

func (r *Runner) TransitionRun(ctx context.Context, cmd api.TransitionRunCommand) error {
	return r.rt.TransitionRun(ctx, cmd)
}

func (r *Runner) Run(ctx context.Context, runID string) (api.Run, error) {
	run, err := r.rt.Run(ctx, runID)
	if err != nil {
		return api.Run{}, err
	}
	return run, nil
}

func (r *Runner) RunEvents(ctx context.Context, runID string) ([]api.Event, error) {
	events, err := r.rt.RunEvents(ctx, runID)
	if err != nil {
		return nil, err
	}
	return events, nil
}

func (r *Runner) ReplayContext(ctx context.Context, runID string, mode api.ReplayMode) (api.Projection, error) {
	projection, err := r.rt.Replay(ctx, runID, api.ReplayMode(mode))
	if err != nil {
		return api.Projection{}, err
	}
	return projection, nil
}

func (r *Runner) ReplayRunStateContext(ctx context.Context, runID string) (api.Projection, error) {
	projection, err := r.rt.ReplayRunState(ctx, runID)
	if err != nil {
		return api.Projection{}, err
	}
	return projection, nil
}

func (r *Runner) Recover(ctx context.Context, runID string) (api.Projection, error) {
	projection, err := r.rt.Recover(ctx, runID)
	if err != nil {
		return api.Projection{}, err
	}
	return projection, nil
}

func (r *Runner) RunTimeline(ctx context.Context, runID string) ([]api.RunTimelineItem, error) {
	items, err := r.rt.RunTimeline(ctx, runID)
	if err != nil {
		return nil, err
	}
	return items, nil
}
