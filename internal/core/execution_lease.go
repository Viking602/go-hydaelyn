package core

import (
	"context"

	"github.com/Viking602/venat/internal/core/model"
	executionsvc "github.com/Viking602/venat/internal/execution"
)

func (r *Runtime) AcquireTaskExecution(ctx context.Context, cmd AcquireTaskExecutionCommand) (model.TaskExecutionLease, bool, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return model.TaskExecutionLease{}, false, err
	}
	acquired, ok := result.(AcquireTaskExecutionResult)
	if !ok {
		return model.TaskExecutionLease{}, false, ErrInvalidCommand
	}
	return acquired.Lease, acquired.Acquired, nil
}

func (r *Runtime) HeartbeatTaskExecution(ctx context.Context, cmd HeartbeatTaskExecutionCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runtime) ReleaseTaskExecution(ctx context.Context, cmd ReleaseTaskExecutionCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}

func registerExecutionUoWCommandHandlers(runtime *Runtime) {
	executionsvc.RegisterHandlers(runtime.commandBus, runtime.newID)
}
