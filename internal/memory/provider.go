package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

type transactionGate chan struct{}

func newTransactionGate() transactionGate {
	gate := make(transactionGate, 1)
	gate <- struct{}{}
	return gate
}

func (g transactionGate) acquire(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-g:
		return nil
	}
}

func (g transactionGate) release() {
	select {
	case g <- struct{}{}:
	default:
		panic("memory provider transaction gate released twice")
	}
}

type Provider struct {
	txGate    transactionGate
	stateLock sync.RWMutex
	committed *State
	hub       *subscriptionHub
}

func NewProvider() *Provider {
	return &Provider{
		txGate:    newTransactionGate(),
		committed: NewState(),
		hub:       newSubscriptionHub(),
	}
}

func (p *Provider) Begin(ctx context.Context) (ports.UnitOfWork, error) {
	if err := p.txGate.acquire(ctx); err != nil {
		return nil, err
	}
	p.stateLock.RLock()
	staged := p.committed.Clone()
	p.stateLock.RUnlock()
	return &UnitOfWork{provider: p, staged: staged}, nil
}

func (p *Provider) SelectItems(_ context.Context, runID string, selector model.BlackboardSelector) ([]model.BlackboardItem, error) {
	p.stateLock.RLock()
	defer p.stateLock.RUnlock()
	return selectBlackboardItems(p.committed, runID, selector), nil
}

func (p *Provider) Subscribe(ctx context.Context, runID string, filter model.BlackboardSelector) (<-chan model.BlackboardItem, func() error, error) {
	return p.hub.Subscribe(ctx, runID, filter)
}

// Notify fans out items to hub subscribers. Used by the external store path
// to emit blackboard notifications after an external commit.
func (p *Provider) Notify(items []model.BlackboardItem) {
	p.hub.Notify(items)
}

func (p *Provider) CommittedSnapshot() *State {
	p.stateLock.RLock()
	defer p.stateLock.RUnlock()
	return p.committed.Clone()
}

type UnitOfWork struct {
	provider *Provider
	staged   *State
	pending  []model.BlackboardItem
	closed   bool
}

var _ ports.UnitOfWork = (*UnitOfWork)(nil)

func (u *UnitOfWork) Runs() ports.RunStore                     { return (*runStore)(u) }
func (u *UnitOfWork) Tasks() ports.TaskStore                   { return (*taskStore)(u) }
func (u *UnitOfWork) Events() ports.EventStore                 { return (*eventStore)(u) }
func (u *UnitOfWork) Blackboard() ports.BlackboardReadWriter   { return (*blackboardStore)(u) }
func (u *UnitOfWork) MailboxOutbox() ports.MailboxOutboxStore  { return (*mailboxStore)(u) }
func (u *UnitOfWork) UserMessages() ports.UserMessageStore     { return (*messageStore)(u) }
func (u *UnitOfWork) Trace() ports.TraceStore                  { return (*traceStore)(u) }
func (u *UnitOfWork) Leases() ports.LeaseStore                 { return (*leaseStore)(u) }
func (u *UnitOfWork) Approvals() ports.ApprovalStore           { return (*approvalStore)(u) }
func (u *UnitOfWork) ResumeTokens() ports.ResumeTokenStore     { return (*resumeTokenStore)(u) }
func (u *UnitOfWork) ActionAttempts() ports.ActionAttemptStore { return (*actionAttemptStore)(u) }
func (u *UnitOfWork) ensureOpen() error {
	if u.closed {
		return fmt.Errorf("memory unit of work closed: %w", model.ErrInvalidCommand)
	}
	return nil
}

func (u *UnitOfWork) Commit(context.Context) error {
	if err := u.ensureOpen(); err != nil {
		return err
	}
	u.provider.stateLock.Lock()
	u.provider.committed = u.staged
	pending := append([]model.BlackboardItem{}, u.pending...)
	u.closed = true
	u.provider.stateLock.Unlock()
	u.provider.txGate.release()
	u.provider.hub.Notify(pending)
	return nil
}

func (u *UnitOfWork) Rollback(context.Context) error {
	if u.closed {
		return nil
	}
	u.closed = true
	u.staged = nil
	u.pending = nil
	u.provider.txGate.release()
	return nil
}

func (u *UnitOfWork) nextID(prefix string) string {
	u.staged.NextID++
	return fmt.Sprintf("%s-%d", prefix, u.staged.NextID)
}
