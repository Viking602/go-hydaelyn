package core

import (
	"context"

	"github.com/Viking602/venat/internal/core/model"
	executionsvc "github.com/Viking602/venat/internal/execution"
)

func (r *Runtime) AcquireTaskExecution(ctx context.Context, cmd AcquireTaskExecutionCommand) (model.TaskExecutionLease, bool, error) {
	result, err := r.AcquireTaskExecutionWithClaims(ctx, cmd)
	if err != nil {
		return model.TaskExecutionLease{}, false, err
	}
	return result.Lease, result.Acquired, nil
}

func (r *Runtime) AcquireTaskExecutionWithClaims(ctx context.Context, cmd AcquireTaskExecutionCommand) (AcquireTaskExecutionResult, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return AcquireTaskExecutionResult{}, err
	}
	acquired, ok := result.(AcquireTaskExecutionResult)
	if !ok {
		return AcquireTaskExecutionResult{}, ErrInvalidCommand
	}
	return acquired, nil
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
	executionsvc.RegisterHandlers(runtime.commandBus, runtime.newID, func(ctx context.Context) (bool, error) {
		capabilities, err := runtime.StoreCapabilities(ctx)
		if err != nil {
			return false, err
		}
		return capabilities.SupportsResourceClaims && capabilities.SupportsTransactions, nil
	})
}
