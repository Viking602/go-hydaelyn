package core

import (
	"context"

	lifecycle "github.com/Viking602/go-hydaelyn/internal/lifecycle"
)

type RequestApprovalCommand struct {
	RunID            string
	TaskID           string
	ActionID         string
	RequesterAgentID string
	Reason           string
	RiskSummary      string
	RequestedAction  string
}

type DecideApprovalCommand struct {
	RunID      string
	ApprovalID string
	DecidedBy  string
	Decision   string
	Reason     string
}

type RecoverResumeTokenCommand struct {
	TokenID string
}

func (RequestApprovalCommand) CommandName() string    { return "approval.request" }
func (DecideApprovalCommand) CommandName() string     { return "approval.decide" }
func (RecoverResumeTokenCommand) CommandName() string { return "resume_token.recover" }

func (r *Runtime) RequestApproval(ctx context.Context, cmd RequestApprovalCommand) (ApprovalRequest, ResumeToken, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return ApprovalRequest{}, ResumeToken{}, err
	}
	items, ok := result.([]any)
	if !ok || len(items) < 2 {
		return ApprovalRequest{}, ResumeToken{}, ErrInvalidCommand
	}
	approval, okApproval := items[0].(ApprovalRequest)
	token, okToken := items[1].(ResumeToken)
	if !okApproval || !okToken {
		return ApprovalRequest{}, ResumeToken{}, ErrInvalidCommand
	}
	return approval, token, nil
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

// newApprovalForTask creates a new ApprovalRequest and ResumeToken for the
// given task. Used by uow_approval_handlers.go (requestApprovalHandler).
func (r *Runtime) newApprovalForTask(task Task, reason, requester string) (ApprovalRequest, ResumeToken) {
	return lifecycle.NewApprovalPair(r.newID, task, reason, requester)
}
