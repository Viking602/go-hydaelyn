package core

import (
	"context"
	"time"
)

func (r *Runtime) AdvanceRun(ctx context.Context, cmd AdvanceRunCommand) (Run, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return Run{}, err
	}
	run, ok := result.(Run)
	if !ok {
		return Run{}, ErrInvalidCommand
	}
	return run, nil
}

func normalizePlannedTask(runID string, task Task) Task {
	if task.RunID == "" {
		task.RunID = runID
	}
	if task.Status == "" {
		task.Status = TaskStatusPlanned
	}
	if task.Version == 0 {
		task.Version = 1
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now().UTC()
	}
	task.UpdatedAt = task.CreatedAt
	return task
}
