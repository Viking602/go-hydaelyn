package mailbox

import (
	"context"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
	corestate "github.com/Viking602/go-hydaelyn/internal/core/state"
)

// LoadDispatchTarget loads run and task for dispatch, checking terminal state.
func LoadDispatchTarget(ctx context.Context, uow ports.FullUnitOfWork, runID, taskID string) (model.Run, model.Task, error) {
	run, err := uow.Runs().LoadRun(ctx, runID)
	if err != nil {
		return model.Run{}, model.Task{}, err
	}
	if corestate.IsTerminalRun(run.Status) {
		return model.Run{}, model.Task{}, model.ErrTerminalState
	}
	task, err := uow.Tasks().LoadTask(ctx, runID, taskID)
	if err != nil {
		return model.Run{}, model.Task{}, err
	}
	if corestate.IsTerminalTask(task.Status) {
		return model.Run{}, model.Task{}, model.ErrTerminalState
	}
	return run, task, nil
}

// EnsureDependenciesReady checks all DependsOn tasks are completed.
func EnsureDependenciesReady(ctx context.Context, uow ports.FullUnitOfWork, task model.Task) error {
	if len(task.DependsOn) == 0 {
		return nil
	}
	tasks, err := uow.Tasks().ListTasks(ctx, task.RunID)
	if err != nil {
		return err
	}
	byID := make(map[string]model.Task, len(tasks))
	for _, item := range tasks {
		byID[item.ID] = item
	}
	ready, fatal := corestate.DependencyGate(task, byID)
	if fatal {
		return model.ErrDependencyFailed
	}
	if !ready {
		return model.ErrDependencyUnmet
	}
	return nil
}
