package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func beginFullUoW(ctx context.Context, provider ports.StoreProvider, fallbackProvider ports.FallbackProvider) (ports.FullUnitOfWork, error) {
	base, err := provider.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if full, ok := base.(ports.FullUnitOfWork); ok {
		return full, nil
	}
	missing := detectMissingOptionalStores(base)
	if fallbackProvider == nil {
		_ = base.Rollback(ctx)
		return nil, fmt.Errorf("store provider does not implement ports.FullUnitOfWork and no FallbackProvider is configured: %w", model.ErrInvalidConfiguration)
	}
	fallback, err := fallbackProvider.BeginFallback(ctx, missing)
	if err != nil {
		_ = base.Rollback(ctx)
		return nil, err
	}
	return wrapPartialUoW(base, fallback, missing), nil
}

func detectMissingOptionalStores(base ports.UnitOfWork) ports.MissingOptionalStores {
	_, hasLeases := base.(ports.LeaseAwareUnitOfWork)
	_, hasApprovals := base.(ports.ApprovalAwareUnitOfWork)
	_, hasActions := base.(ports.ActionAwareUnitOfWork)
	return ports.MissingOptionalStores{
		Leases:         !hasLeases,
		Approvals:      !hasApprovals,
		ResumeTokens:   !hasApprovals,
		ActionAttempts: !hasActions,
	}
}

func wrapPartialUoW(base ports.UnitOfWork, fallback ports.FallbackTx, missing ports.MissingOptionalStores) ports.FullUnitOfWork {
	wrapped := &compatFullUnitOfWork{base: base, fallback: fallback}
	if leaseAware, ok := base.(ports.LeaseAwareUnitOfWork); ok && !missing.Leases {
		wrapped.leases = leaseAware.Leases()
	} else {
		wrapped.leases = fallback.Leases()
	}
	if approvalAware, ok := base.(ports.ApprovalAwareUnitOfWork); ok && !missing.Approvals {
		wrapped.approvals = approvalAware.Approvals()
	} else {
		wrapped.approvals = fallback.Approvals()
	}
	if approvalAware, ok := base.(ports.ApprovalAwareUnitOfWork); ok && !missing.ResumeTokens {
		wrapped.resumeTokens = approvalAware.ResumeTokens()
	} else {
		wrapped.resumeTokens = fallback.ResumeTokens()
	}
	if actionAware, ok := base.(ports.ActionAwareUnitOfWork); ok && !missing.ActionAttempts {
		wrapped.actionAttempts = actionAware.ActionAttempts()
	} else {
		wrapped.actionAttempts = fallback.ActionAttempts()
	}
	return wrapped
}

type compatFullUnitOfWork struct {
	base           ports.UnitOfWork
	fallback       ports.FallbackTx
	leases         ports.LeaseStore
	approvals      ports.ApprovalStore
	resumeTokens   ports.ResumeTokenStore
	actionAttempts ports.ActionAttemptStore
}

func (c *compatFullUnitOfWork) Runs() ports.RunStore                   { return c.base.Runs() }
func (c *compatFullUnitOfWork) Tasks() ports.TaskStore                 { return c.base.Tasks() }
func (c *compatFullUnitOfWork) Events() ports.EventStore               { return c.base.Events() }
func (c *compatFullUnitOfWork) Blackboard() ports.BlackboardReadWriter { return c.base.Blackboard() }
func (c *compatFullUnitOfWork) MailboxOutbox() ports.MailboxOutboxStore {
	return c.base.MailboxOutbox()
}
func (c *compatFullUnitOfWork) UserMessages() ports.UserMessageStore { return c.base.UserMessages() }
func (c *compatFullUnitOfWork) Trace() ports.TraceStore              { return c.base.Trace() }
func (c *compatFullUnitOfWork) Leases() ports.LeaseStore             { return c.leases }
func (c *compatFullUnitOfWork) Approvals() ports.ApprovalStore       { return c.approvals }
func (c *compatFullUnitOfWork) ResumeTokens() ports.ResumeTokenStore { return c.resumeTokens }
func (c *compatFullUnitOfWork) ActionAttempts() ports.ActionAttemptStore {
	return c.actionAttempts
}

func (c *compatFullUnitOfWork) Commit(ctx context.Context) error {
	if err := c.base.Commit(ctx); err != nil {
		_ = c.fallback.Rollback(ctx)
		return err
	}
	if err := c.fallback.Commit(ctx); err != nil {
		panic(fmt.Sprintf("invariant violation: fallback commit failed after base committed: %v", err))
	}
	return nil
}

func (c *compatFullUnitOfWork) Rollback(ctx context.Context) error {
	return errors.Join(c.base.Rollback(ctx), c.fallback.Rollback(ctx))
}
