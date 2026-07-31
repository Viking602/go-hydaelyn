package core

import (
	"context"

	approvalsvc "github.com/Viking602/venat/internal/approval"
	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/lifecycle"
)

type (
	RequestApprovalCommand    = approvalsvc.RequestApprovalCommand
	DecideApprovalCommand     = approvalsvc.DecideApprovalCommand
	RecoverResumeTokenCommand = approvalsvc.RecoverResumeTokenCommand
	RequestApprovalResult     = approvalsvc.RequestApprovalResult
)

func (r *Runtime) RequestApproval(ctx context.Context, cmd RequestApprovalCommand) (model.ApprovalRequest, model.ResumeToken, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return model.ApprovalRequest{}, model.ResumeToken{}, err
	}
	requested, ok := result.(RequestApprovalResult)
	if !ok {
		return model.ApprovalRequest{}, model.ResumeToken{}, ErrInvalidCommand
	}
	return requested.Approval, requested.Token, nil
}

func (r *Runtime) DecideApproval(ctx context.Context, cmd DecideApprovalCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runtime) RecoverResumeToken(ctx context.Context, cmd RecoverResumeTokenCommand) (model.ResumeToken, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return model.ResumeToken{}, err
	}
	token, ok := result.(model.ResumeToken)
	if !ok {
		return model.ResumeToken{}, ErrInvalidCommand
	}
	return token, nil
}

func registerApprovalUoWCommandHandlers(runtime *Runtime) {
	approvalsvc.RegisterHandlers(runtime.commandBus, approvalsvc.HandlerOptions{
		NewApproval: runtime.newApprovalForTask,
		NewID:       runtime.newID,
	})
}

// newApprovalForTask creates a new ApprovalRequest and ResumeToken for the
// given task. Domain handlers receive it as an injected ApprovalFactory.
func (r *Runtime) newApprovalForTask(task model.Task, reason, requester string) (model.ApprovalRequest, model.ResumeToken) {
	return lifecycle.NewApprovalPair(r.newID, task, reason, requester)
}

// PendingResumeTokens lists unconsumed resume tokens matching sel — the
// crash-recovery enumeration primitive: a restarting host lists pending
// tokens and feeds each to RecoverResumeToken instead of hand-rolling
// store access.
func (r *Runtime) PendingResumeTokens(ctx context.Context, sel model.ResumeTokenSelector) ([]model.ResumeToken, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	return uow.ResumeTokens().ListPending(ctx, sel)
}
