package hydaelyn

import (
	"context"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/internal/core/adapter"
)

func (r *Runner) CreateTask(ctx context.Context, cmd api.CreateTaskCommand) (api.Task, error) {
	task, err := r.rt.CreateTask(ctx, adapter.CreateTaskCommandToCore(cmd))
	if err != nil {
		return api.Task{}, adapter.ErrorToAPI(err)
	}
	return adapter.TaskFromModel(task), nil
}

func (r *Runner) Task(ctx context.Context, runID, taskID string) (api.Task, error) {
	task, err := r.rt.Task(ctx, runID, taskID)
	if err != nil {
		return api.Task{}, adapter.ErrorToAPI(err)
	}
	return adapter.TaskFromModel(task), nil
}

func (r *Runner) ReadyTasks(runID string) []api.Task {
	return adapter.TasksFromModel(r.rt.ReadyTasks(context.Background(), runID))
}

func (r *Runner) TransitionTask(ctx context.Context, cmd api.TransitionTaskCommand) error {
	return adapter.ErrorToAPI(r.rt.TransitionTask(ctx, adapter.TransitionTaskCommandToCore(cmd)))
}

func (r *Runner) SaveTask(ctx context.Context, task api.Task) error {
	return adapter.ErrorToAPI(r.rt.SaveTask(ctx, adapter.TaskToModel(task)))
}

func (r *Runner) LoadTask(ctx context.Context, runID, taskID string) (api.Task, error) {
	task, err := r.rt.LoadTask(ctx, runID, taskID)
	if err != nil {
		return api.Task{}, adapter.ErrorToAPI(err)
	}
	return adapter.TaskFromModel(task), nil
}

func (r *Runner) ListTasks(ctx context.Context, runID string) ([]api.Task, error) {
	tasks, err := r.rt.ListTasks(ctx, runID)
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return adapter.TasksFromModel(tasks), nil
}
