package core

import (
	"context"

	actionsvc "github.com/Viking602/go-hydaelyn/internal/action"
)

type (
	StartActionAttemptCommand    = actionsvc.StartActionAttemptCommand
	CompleteActionAttemptCommand = actionsvc.CompleteActionAttemptCommand
	completeActionAttemptResult  = actionsvc.CompleteAttemptResult
)

func (r *Runtime) StartActionAttempt(ctx context.Context, cmd StartActionAttemptCommand) (ActionAttempt, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return ActionAttempt{}, err
	}
	attempt, ok := result.(ActionAttempt)
	if !ok {
		return ActionAttempt{}, ErrInvalidCommand
	}
	return attempt, nil
}

func (r *Runtime) CompleteActionAttempt(ctx context.Context, cmd CompleteActionAttemptCommand) (ActionAttempt, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return ActionAttempt{}, err
	}
	attempt, ok := result.(ActionAttempt)
	if !ok {
		return ActionAttempt{}, ErrInvalidCommand
	}
	return attempt, nil
}

func registerActionUoWCommandHandlers(runtime *Runtime) {
	actionsvc.RegisterHandlers(runtime.commandBus, actionsvc.HandlerOptions{
		NewID:     runtime.newID,
		Authorize: runtime.authorizeUoW,
	})
}
