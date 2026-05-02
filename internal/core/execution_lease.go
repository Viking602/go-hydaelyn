package core

import (
	"context"

	executionsvc "github.com/Viking602/go-hydaelyn/internal/execution"
)

func (r *Runtime) AcquireTaskExecution(ctx context.Context, cmd AcquireTaskExecutionCommand) (TaskExecutionLease, bool, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return TaskExecutionLease{}, false, err
	}
	acquired, ok := result.(AcquireTaskExecutionResult)
	if !ok {
		return TaskExecutionLease{}, false, ErrInvalidCommand
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
