package memory

import (
	"context"
	"sync"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

type FallbackProvider struct {
	txGate    transactionGate
	stateLock sync.RWMutex
	committed *State
}

func NewFallbackProvider() *FallbackProvider {
	return &FallbackProvider{
		txGate:    newTransactionGate(),
		committed: NewState(),
	}
}

func (p *FallbackProvider) BeginFallback(ctx context.Context, _ ports.MissingOptionalStores) (ports.FallbackTx, error) {
	if err := p.txGate.acquire(ctx); err != nil {
		return nil, err
	}
	p.stateLock.RLock()
	staged := p.committed.Clone()
	p.stateLock.RUnlock()
	return &FallbackTx{provider: p, staged: staged}, nil
}

func (p *FallbackProvider) Snapshot() *State {
	p.stateLock.RLock()
	defer p.stateLock.RUnlock()
	return p.committed.Clone()
}

type FallbackTx struct {
	provider *FallbackProvider
	staged   *State
	closed   bool
}

var _ ports.FallbackTx = (*FallbackTx)(nil)

func (t *FallbackTx) Leases() ports.LeaseStore             { return (*fallbackLeaseStore)(t) }
func (t *FallbackTx) Approvals() ports.ApprovalStore       { return (*fallbackApprovalStore)(t) }
func (t *FallbackTx) ResumeTokens() ports.ResumeTokenStore { return (*fallbackResumeTokenStore)(t) }
func (t *FallbackTx) ActionAttempts() ports.ActionAttemptStore {
	return (*fallbackActionAttemptStore)(t)
}

func (t *FallbackTx) Commit(context.Context) error {
	if t.closed {
		return nil
	}
	t.provider.stateLock.Lock()
	t.provider.committed = t.staged
	t.closed = true
	t.provider.stateLock.Unlock()
	t.provider.txGate.release()
	return nil
}

func (t *FallbackTx) Rollback(context.Context) error {
	if t.closed {
		return nil
	}
	t.closed = true
	t.staged = nil
	t.provider.txGate.release()
	return nil
}

func (t *FallbackTx) ensureOpen() error {
	if t.closed {
		return model.ErrInvalidCommand
	}
	return nil
}

type fallbackLeaseStore FallbackTx

type fallbackApprovalStore FallbackTx

type fallbackResumeTokenStore FallbackTx

type fallbackActionAttemptStore FallbackTx

func (s *fallbackLeaseStore) tx() *FallbackTx { return (*FallbackTx)(s) }

func (s *fallbackApprovalStore) tx() *FallbackTx { return (*FallbackTx)(s) }

func (s *fallbackResumeTokenStore) tx() *FallbackTx { return (*FallbackTx)(s) }

func (s *fallbackActionAttemptStore) tx() *FallbackTx { return (*FallbackTx)(s) }

func (s *fallbackLeaseStore) SaveLease(_ context.Context, lease model.TaskExecutionLease) error {
	tx := s.tx()
	if err := tx.ensureOpen(); err != nil {
		return err
	}
	tx.staged.Leases[lease.ID] = lease
	key := activeLeaseKey(lease.RunID, lease.TaskID)
	if lease.Status == model.LeaseStatusActive {
		tx.staged.ActiveLeaseByTask[key] = lease.ID
	} else if tx.staged.ActiveLeaseByTask[key] == lease.ID {
		delete(tx.staged.ActiveLeaseByTask, key)
	}
	return nil
}

func (s *fallbackLeaseStore) LoadLease(_ context.Context, leaseID string) (model.TaskExecutionLease, error) {
	tx := s.tx()
	if err := tx.ensureOpen(); err != nil {
		return model.TaskExecutionLease{}, err
	}
	lease, ok := tx.staged.Leases[leaseID]
	if !ok {
		return model.TaskExecutionLease{}, model.ErrNotFound
	}
	return lease, nil
}

func (s *fallbackLeaseStore) ActiveLeaseForTask(_ context.Context, runID, taskID string) (model.TaskExecutionLease, bool, error) {
	tx := s.tx()
	if err := tx.ensureOpen(); err != nil {
		return model.TaskExecutionLease{}, false, err
	}
	leaseID := tx.staged.ActiveLeaseByTask[activeLeaseKey(runID, taskID)]
	if leaseID == "" {
		return model.TaskExecutionLease{}, false, nil
	}
	lease, ok := tx.staged.Leases[leaseID]
	return lease, ok, nil
}

func (s *fallbackApprovalStore) SaveApproval(_ context.Context, approval model.ApprovalRequest) error {
	tx := s.tx()
	if err := tx.ensureOpen(); err != nil {
		return err
	}
	tx.staged.Approvals[approval.ApprovalID] = approval
	return nil
}

func (s *fallbackApprovalStore) LoadApproval(_ context.Context, approvalID string) (model.ApprovalRequest, error) {
	tx := s.tx()
	if err := tx.ensureOpen(); err != nil {
		return model.ApprovalRequest{}, err
	}
	approval, ok := tx.staged.Approvals[approvalID]
	if !ok {
		return model.ApprovalRequest{}, model.ErrNotFound
	}
	return approval, nil
}

func (s *fallbackResumeTokenStore) SaveResumeToken(_ context.Context, token model.ResumeToken) error {
	tx := s.tx()
	if err := tx.ensureOpen(); err != nil {
		return err
	}
	tx.staged.ResumeTokens[token.TokenID] = token
	return nil
}

func (s *fallbackResumeTokenStore) LoadResumeToken(_ context.Context, tokenID string) (model.ResumeToken, error) {
	tx := s.tx()
	if err := tx.ensureOpen(); err != nil {
		return model.ResumeToken{}, err
	}
	token, ok := tx.staged.ResumeTokens[tokenID]
	if !ok {
		return model.ResumeToken{}, model.ErrNotFound
	}
	return token, nil
}

func (s *fallbackActionAttemptStore) SaveActionAttempt(_ context.Context, attempt model.ActionAttempt) error {
	tx := s.tx()
	if err := tx.ensureOpen(); err != nil {
		return err
	}
	tx.staged.ActionAttempts[attempt.AttemptID] = attempt
	return nil
}

func (s *fallbackActionAttemptStore) LoadActionAttempt(_ context.Context, attemptID string) (model.ActionAttempt, error) {
	tx := s.tx()
	if err := tx.ensureOpen(); err != nil {
		return model.ActionAttempt{}, err
	}
	attempt, ok := tx.staged.ActionAttempts[attemptID]
	if !ok {
		return model.ActionAttempt{}, model.ErrNotFound
	}
	return attempt, nil
}
