package core

import (
	"context"

	actionsvc "github.com/Viking602/venat/internal/action"
	"github.com/Viking602/venat/internal/core/model"
)

type (
	StartActionAttemptCommand    = actionsvc.StartActionAttemptCommand
	CompleteActionAttemptCommand = actionsvc.CompleteActionAttemptCommand
	ResolveActionAttemptCommand  = actionsvc.ResolveActionAttemptCommand
	completeActionAttemptResult  = actionsvc.CompleteAttemptResult
	resolveActionAttemptResult   = actionsvc.ResolveAttemptResult
)

func (r *Runtime) StartActionAttempt(ctx context.Context, cmd StartActionAttemptCommand) (model.ActionAttempt, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return model.ActionAttempt{}, err
	}
	attempt, ok := result.(model.ActionAttempt)
	if !ok {
		return model.ActionAttempt{}, ErrInvalidCommand
	}
	return attempt, nil
}

func (r *Runtime) CompleteActionAttempt(ctx context.Context, cmd CompleteActionAttemptCommand) (model.ActionAttempt, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return model.ActionAttempt{}, err
	}
	attempt, ok := result.(model.ActionAttempt)
	if !ok {
		return model.ActionAttempt{}, ErrInvalidCommand
	}
	return attempt, nil
}

func (r *Runtime) ResolveActionAttempt(ctx context.Context, cmd ResolveActionAttemptCommand) (model.ActionAttempt, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return model.ActionAttempt{}, err
	}
	attempt, ok := result.(model.ActionAttempt)
	if !ok {
		return model.ActionAttempt{}, ErrInvalidCommand
	}
	return attempt, nil
}

func (r *Runtime) ListActionAttempts(ctx context.Context, selector model.ActionAttemptSelector) ([]model.ActionAttempt, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = done() }()
	return uow.ActionAttempts().ListActionAttempts(ctx, selector)
}

func registerActionUoWCommandHandlers(runtime *Runtime) {
	actionsvc.RegisterHandlers(runtime.commandBus, actionsvc.HandlerOptions{
		NewID:     runtime.newID,
		Authorize: runtime.authorizeUoW,
	})
}
