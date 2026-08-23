// Package memory is the process-local development and test StoreProvider.
//
// It is not crash-durable and is not a Position D reference
// implementation (ADR-012). Production hosts must supply their own
// api.StoreProvider. Optional Limits only cap per-run growth inside one
// process.
package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
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
		// Idempotent: a panicked or double-closed UoW must not deadlock
		// later writers, and must not crash the process.
	}
}

// Limits optionally bounds per-run in-memory growth. Zero means unlimited.
// The memory provider is process-local and not crash-durable; these caps
// only protect a single process from unbounded event/trace accumulation.
type Limits struct {
	MaxEventsPerRun     int
	MaxTraceSpansPerRun int
}

type Provider struct {
	txGate    transactionGate
	stateLock sync.RWMutex
	committed *State
	hub       *subscriptionHub
	limits    Limits
}

func NewProvider() *Provider {
	return NewProviderWithLimits(Limits{})
}

func NewProviderWithLimits(limits Limits) *Provider {
	return &Provider{
		txGate:    newTransactionGate(),
		committed: NewState(),
		hub:       newSubscriptionHub(),
		limits:    limits,
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

// BeginRead opens a snapshot UoW that does not take the write gate, so
// readers do not serialize behind writers. Commit is rejected.
func (p *Provider) BeginRead(context.Context) (ports.UnitOfWork, error) {
	p.stateLock.RLock()
	staged := p.committed.Clone()
	p.stateLock.RUnlock()
	return &UnitOfWork{provider: p, staged: staged, readOnly: true}, nil
}

// DroppedCount is the number of blackboard fan-out items discarded because
// a subscriber buffer was full.
func (p *Provider) DroppedCount() uint64 {
	return p.hub.DroppedCount()
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

// Capabilities reports the optional features this provider supports.
// Memory: transactional (single-writer via gate), blackboard subscribe via
// the local hub, list-pending via in-memory scan; no concurrent writers, no
// dead-letter requeue. Satisfies ports.CapabilityReporter.
func (p *Provider) Capabilities(context.Context) (ports.StoreCapabilities, error) {
	return ports.StoreCapabilities{
		SupportsTransactions:          true,
		SupportsBlackboardSubscribe:   true,
		SupportsListPending:           true,
		SupportsConcurrentWriters:     false,
		SupportsDeadLetterRequeue:     false,
		SupportsDefinitionSnapshots:   true,
		SupportsAdmissionReservations: true,
		SupportsResourceClaims:        true,
	}, nil
}

// Close releases provider-scoped resources. Memory provider keeps state in
// process memory, so Close is a no-op. Satisfies ports.ProviderCloser so the
// runtime can call it during shutdown without a type-specific path.
func (p *Provider) Close(context.Context) error { return nil }

type UnitOfWork struct {
	provider *Provider
	staged   *State
	pending  []model.BlackboardItem
	closed   bool
	readOnly bool
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
func (u *UnitOfWork) AgentProfiles() ports.AgentProfileStore   { return (*agentProfileStore)(u) }
func (u *UnitOfWork) CapabilityCatalog() ports.CapabilityStore { return (*capabilityStore)(u) }
func (u *UnitOfWork) UsageRecords() ports.UsageStore           { return (*usageStore)(u) }
func (u *UnitOfWork) DeadLetters() ports.DeadLetterStore       { return (*deadLetterStore)(u) }
func (u *UnitOfWork) Handoffs() ports.HandoffStore             { return (*handoffStore)(u) }
func (u *UnitOfWork) TeamStates() ports.TeamStateStore         { return (*teamStateStore)(u) }
func (u *UnitOfWork) AgentInstances() ports.AgentInstanceStore { return (*agentInstanceStore)(u) }
func (u *UnitOfWork) AgentDefinitions() ports.AgentDefinitionStore {
	return (*agentDefinitionStore)(u)
}

func (u *UnitOfWork) AdmissionReservations() ports.AdmissionReservationStore {
	return (*admissionReservationStore)(u)
}

func (u *UnitOfWork) ResourceClaims() ports.ResourceClaimStore {
	return (*resourceClaimStore)(u)
}

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
	if u.readOnly {
		return fmt.Errorf("memory unit of work is read-only: %w", model.ErrInvalidCommand)
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
	if !u.readOnly {
		u.provider.txGate.release()
	}
	return nil
}

func (u *UnitOfWork) nextID(prefix string) string {
	u.staged.NextID++
	return fmt.Sprintf("%s-%d", prefix, u.staged.NextID)
}
