package venat

import (
	"context"

	"github.com/Viking602/venat/api"
)

func (r *Runner) CreateTask(ctx context.Context, cmd api.CreateTaskCommand) (api.Task, error) {
	task, err := r.rt.CreateTask(ctx, cmd)
	if err != nil {
		return api.Task{}, err
	}
	return task, nil
}

func (r *Runner) Task(ctx context.Context, runID, taskID string) (api.Task, error) {
	task, err := r.rt.Task(ctx, runID, taskID)
	if err != nil {
		return api.Task{}, err
	}
	return task, nil
}

func (r *Runner) ReadyTasksContext(ctx context.Context, runID string) ([]api.Task, error) {
	tasks, err := r.rt.ReadyTasks(ctx, runID)
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

func (r *Runner) TransitionTask(ctx context.Context, cmd api.TransitionTaskCommand) error {
	return r.rt.TransitionTask(ctx, cmd)
}

func (r *Runner) SaveTask(ctx context.Context, task api.Task) error {
	return r.rt.SaveTask(ctx, task)
}

func (r *Runner) LoadTask(ctx context.Context, runID, taskID string) (api.Task, error) {
	task, err := r.rt.LoadTask(ctx, runID, taskID)
	if err != nil {
		return api.Task{}, err
	}
	return task, nil
}

func (r *Runner) ListTasks(ctx context.Context, runID string) ([]api.Task, error) {
	tasks, err := r.rt.ListTasks(ctx, runID)
	if err != nil {
		return nil, err
	}
	return tasks, nil
}
