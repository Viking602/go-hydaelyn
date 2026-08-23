package core

import (
	"context"

	"github.com/Viking602/venat/api"
	actionsvc "github.com/Viking602/venat/internal/action"
)

type (
	StartActionAttemptCommand    = actionsvc.StartActionAttemptCommand
	CompleteActionAttemptCommand = actionsvc.CompleteActionAttemptCommand
	ResolveActionAttemptCommand  = actionsvc.ResolveActionAttemptCommand
	completeActionAttemptResult  = actionsvc.CompleteAttemptResult
	resolveActionAttemptResult   = actionsvc.ResolveAttemptResult
)

func (r *Runtime) StartActionAttempt(ctx context.Context, cmd StartActionAttemptCommand) (api.ActionAttempt, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return api.ActionAttempt{}, err
	}
	attempt, ok := result.(api.ActionAttempt)
	if !ok {
		return api.ActionAttempt{}, ErrInvalidCommand
	}
	return attempt, nil
}

func (r *Runtime) CompleteActionAttempt(ctx context.Context, cmd CompleteActionAttemptCommand) (api.ActionAttempt, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return api.ActionAttempt{}, err
	}
	attempt, ok := result.(api.ActionAttempt)
	if !ok {
		return api.ActionAttempt{}, ErrInvalidCommand
	}
	return attempt, nil
}

func (r *Runtime) ResolveActionAttempt(ctx context.Context, cmd ResolveActionAttemptCommand) (api.ActionAttempt, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return api.ActionAttempt{}, err
	}
	attempt, ok := result.(api.ActionAttempt)
	if !ok {
		return api.ActionAttempt{}, ErrInvalidCommand
	}
	return attempt, nil
}

func (r *Runtime) ListActionAttempts(ctx context.Context, selector api.ActionAttemptSelector) ([]api.ActionAttempt, error) {
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
