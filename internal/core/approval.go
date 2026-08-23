package core

import (
	"context"
	"fmt"

	"github.com/Viking602/venat/api"
	approvalsvc "github.com/Viking602/venat/internal/approval"
	"github.com/Viking602/venat/internal/lifecycle"
)

type (
	RequestApprovalCommand    = approvalsvc.RequestApprovalCommand
	DecideApprovalCommand     = approvalsvc.DecideApprovalCommand
	RecoverResumeTokenCommand = approvalsvc.RecoverResumeTokenCommand
	RequestApprovalResult     = approvalsvc.RequestApprovalResult
)

func (r *Runtime) RequestApproval(ctx context.Context, cmd RequestApprovalCommand) (api.ApprovalRequest, api.ResumeToken, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return api.ApprovalRequest{}, api.ResumeToken{}, err
	}
	requested, ok := result.(RequestApprovalResult)
	if !ok {
		return api.ApprovalRequest{}, api.ResumeToken{}, ErrInvalidCommand
	}
	return requested.Approval, requested.Token, nil
}

func (r *Runtime) DecideApproval(ctx context.Context, cmd DecideApprovalCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runtime) RecoverResumeToken(ctx context.Context, cmd RecoverResumeTokenCommand) (api.ResumeToken, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return api.ResumeToken{}, err
	}
	token, ok := result.(api.ResumeToken)
	if !ok {
		return api.ResumeToken{}, ErrInvalidCommand
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
func (r *Runtime) newApprovalForTask(task api.Task, reason, requester string) (api.ApprovalRequest, api.ResumeToken) {
	return lifecycle.NewApprovalPair(r.newID, task, reason, requester)
}

// PendingResumeTokens lists unconsumed resume tokens matching sel — the
// crash-recovery enumeration primitive: a restarting host lists pending
// tokens and feeds each to RecoverResumeToken instead of hand-rolling
// store access.
func (r *Runtime) PendingResumeTokens(ctx context.Context, sel api.ResumeTokenSelector) (tokens []api.ResumeToken, err error) {
	capabilities, err := r.StoreCapabilities(ctx)
	if err != nil {
		return nil, err
	}
	if !capabilities.SupportsListPending {
		return nil, fmt.Errorf("resume token enumeration is not supported: %w", api.ErrInvalidConfiguration)
	}
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return nil, err
	}
	defer joinReadCleanup(&err, done)
	return uow.ResumeTokens().ListPending(ctx, sel)
}

// ResumeTokens returns unconsumed resume tokens keyed by TokenID.
// Store errors are returned; an empty map means the store confirmed there
// are no pending tokens. Stores that do not advertise SupportsListPending
// return ErrInvalidConfiguration. Prefer PendingResumeTokens for new call sites.
func (r *Runtime) ResumeTokens(ctx context.Context) (map[string]api.ResumeToken, error) {
	tokens, err := r.PendingResumeTokens(ctx, api.ResumeTokenSelector{})
	if err != nil {
		return nil, err
	}
	out := make(map[string]api.ResumeToken, len(tokens))
	for _, token := range tokens {
		if token.TokenID == "" {
			continue
		}
		out[token.TokenID] = token
	}
	return out, nil
}
