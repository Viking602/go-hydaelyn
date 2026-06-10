package hydaelyn

import (
	"context"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/internal/core"
	"github.com/Viking602/go-hydaelyn/internal/core/adapter"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

func (r *Runner) QueueRun(ctx context.Context, cmd api.StartRunCommand) (api.Run, error) {
	run, err := r.rt.QueueRun(ctx, adapter.StartRunCommandToCore(cmd))
	if err != nil {
		return api.Run{}, adapter.ErrorToAPI(err)
	}
	return adapter.RunFromModel(run), nil
}

func (r *Runner) StartRun(ctx context.Context, cmd api.StartRunCommand) (api.Run, api.Task, error) {
	run, task, err := r.rt.StartRun(ctx, adapter.StartRunCommandToCore(cmd))
	if err != nil {
		return api.Run{}, api.Task{}, adapter.ErrorToAPI(err)
	}
	return adapter.RunFromModel(run), adapter.TaskFromModel(task), nil
}

func (r *Runner) AdvanceRun(ctx context.Context, cmd api.AdvanceRunCommand) (api.Run, error) {
	run, err := r.rt.AdvanceRun(ctx, core.AdvanceRunCommand{RunID: cmd.RunID})
	if err != nil {
		return api.Run{}, adapter.ErrorToAPI(err)
	}
	return adapter.RunFromModel(run), nil
}

func (r *Runner) TransitionRun(ctx context.Context, cmd api.TransitionRunCommand) error {
	return adapter.ErrorToAPI(r.rt.TransitionRun(ctx, adapter.TransitionRunCommandToCore(cmd)))
}

func (r *Runner) Run(ctx context.Context, runID string) (api.Run, error) {
	run, err := r.rt.Run(ctx, runID)
	if err != nil {
		return api.Run{}, adapter.ErrorToAPI(err)
	}
	return adapter.RunFromModel(run), nil
}

func (r *Runner) RunEvents(ctx context.Context, runID string) ([]api.Event, error) {
	events, err := r.rt.RunEvents(ctx, runID)
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return adapter.EventsFromModel(events), nil
}

func (r *Runner) Events(runID string) []api.Event {
	return adapter.EventsFromModel(r.rt.Events(context.Background(), runID))
}

func (r *Runner) Replay(runID string, mode api.ReplayMode) (api.Projection, error) {
	projection, err := r.rt.Replay(context.Background(), runID, model.ReplayMode(mode))
	if err != nil {
		return api.Projection{}, adapter.ErrorToAPI(err)
	}
	return adapter.ProjectionFromModel(projection), nil
}

func (r *Runner) ReplayRunState(runID string) (api.Projection, error) {
	projection, err := r.rt.ReplayRunState(context.Background(), runID)
	if err != nil {
		return api.Projection{}, adapter.ErrorToAPI(err)
	}
	return adapter.ProjectionFromModel(projection), nil
}

func (r *Runner) Recover(ctx context.Context, runID string) (api.Projection, error) {
	projection, err := r.rt.Recover(ctx, runID)
	if err != nil {
		return api.Projection{}, adapter.ErrorToAPI(err)
	}
	return adapter.ProjectionFromModel(projection), nil
}

func (r *Runner) RunTimeline(ctx context.Context, runID string) ([]api.RunTimelineItem, error) {
	items, err := r.rt.RunTimeline(ctx, runID)
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return adapter.RunTimelineItemsFromModel(items), nil
}
