package core

import (
	"context"

	approvalsvc "github.com/Viking602/go-hydaelyn/internal/approval"
	lifecycle "github.com/Viking602/go-hydaelyn/internal/lifecycle"
)

type (
	RequestApprovalCommand    = approvalsvc.RequestApprovalCommand
	DecideApprovalCommand     = approvalsvc.DecideApprovalCommand
	RecoverResumeTokenCommand = approvalsvc.RecoverResumeTokenCommand
	RequestApprovalResult     = approvalsvc.RequestApprovalResult
)

func (r *Runtime) RequestApproval(ctx context.Context, cmd RequestApprovalCommand) (ApprovalRequest, ResumeToken, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return ApprovalRequest{}, ResumeToken{}, err
	}
	requested, ok := result.(RequestApprovalResult)
	if !ok {
		return ApprovalRequest{}, ResumeToken{}, ErrInvalidCommand
	}
	return requested.Approval, requested.Token, nil
}

func (r *Runtime) DecideApproval(ctx context.Context, cmd DecideApprovalCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runtime) RecoverResumeToken(ctx context.Context, cmd RecoverResumeTokenCommand) (ResumeToken, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return ResumeToken{}, err
	}
	token, ok := result.(ResumeToken)
	if !ok {
		return ResumeToken{}, ErrInvalidCommand
	}
	return token, nil
}

func registerApprovalUoWCommandHandlers(runtime *Runtime) {
	approvalsvc.RegisterHandlers(runtime.commandBus, approvalsvc.HandlerOptions{
		NewApproval: runtime.newApprovalForTask,
	})
}

// newApprovalForTask creates a new ApprovalRequest and ResumeToken for the
// given task. Domain handlers receive it as an injected ApprovalFactory.
func (r *Runtime) newApprovalForTask(task Task, reason, requester string) (ApprovalRequest, ResumeToken) {
	return lifecycle.NewApprovalPair(r.newID, task, reason, requester)
}
