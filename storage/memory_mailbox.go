package storage

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Viking602/go-hydaelyn/mailbox"
)

// memoryMailboxStore is the in-process MailboxStore used by MemoryDriver.
// Locking granularity is a single RWMutex — recipients are expected in the
// hundreds, not millions, per process; fine-grained locks add complexity
// without meaningful benefit for the memory driver.
type memoryMailboxStore struct {
	mu            sync.RWMutex
	envelopes     map[string]*mailbox.Envelope
	bySequence    map[string]int64    // recipientKey -> next sequence
	byRecipient   map[string][]string // recipientKey -> envelope IDs (all states, insertion order)
	byCorrelation map[string][]string // correlationKey -> envelope IDs
	idSeq         uint64
}

func newMemoryMailboxStore() *memoryMailboxStore {
	return &memoryMailboxStore{
		envelopes:     map[string]*mailbox.Envelope{},
		bySequence:    map[string]int64{},
		byRecipient:   map[string][]string{},
		byCorrelation: map[string][]string{},
	}
}

func recipientKey(teamRunID, agentID string) string {
	return teamRunID + "\x00" + agentID
}

func correlationKey(teamRunID, correlationID string) string {
	return teamRunID + "\x00" + correlationID
}

func (s *memoryMailboxStore) Append(_ context.Context, env mailbox.Envelope) (mailbox.Envelope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(env.ID) == "" {
		s.idSeq++
		env.ID = fmt.Sprintf("env-%d-%d", time.Now().UnixNano(), s.idSeq)
	}
	if _, ok := s.envelopes[env.ID]; ok {
		return mailbox.Envelope{}, fmt.Errorf("%w: duplicate envelope id %q", mailbox.ErrConflict, env.ID)
	}

	if env.To.Kind != mailbox.AddressKindAgent {
		return mailbox.Envelope{}, fmt.Errorf("%w: stored envelopes must be kind=agent", mailbox.ErrInvalidAddress)
	}
	rkey := recipientKey(env.TeamRunID, env.To.AgentID)

	if env.Sequence <= 0 {
		s.bySequence[rkey]++
		env.Sequence = s.bySequence[rkey]
	} else if env.Sequence > s.bySequence[rkey] {
		s.bySequence[rkey] = env.Sequence
	}

	if env.CreatedAt.IsZero() {
		env.CreatedAt = time.Now().UTC()
	}
	if env.State == "" {
		env.State = mailbox.EnvelopePending
	}
	if env.Version <= 0 {
		env.Version = 1
	}

	stored := env
	s.envelopes[env.ID] = &stored
	s.byRecipient[rkey] = append(s.byRecipient[rkey], env.ID)

	if strings.TrimSpace(env.CorrelationID) != "" {
		ck := correlationKey(env.TeamRunID, env.CorrelationID)
		s.byCorrelation[ck] = append(s.byCorrelation[ck], env.ID)
	}

	return stored, nil
}

func (s *memoryMailboxStore) NextSequence(_ context.Context, teamRunID, agentID string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bySequence[recipientKey(teamRunID, agentID)]++
	return s.bySequence[recipientKey(teamRunID, agentID)], nil
}

func (s *memoryMailboxStore) ListPending(_ context.Context, teamRunID, agentID string, limit int) ([]mailbox.Envelope, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := s.byRecipient[recipientKey(teamRunID, agentID)]
	out := make([]mailbox.Envelope, 0, len(ids))
	for _, id := range ids {
		env := s.envelopes[id]
		if env == nil || env.State != mailbox.EnvelopePending {
			continue
		}
		out = append(out, *env)
	}
	// Stable: priority desc, sequence asc.
	sort.SliceStable(out, func(i, j int) bool {
		pi := priorityRankForEnvelope(out[i])
		pj := priorityRankForEnvelope(out[j])
		if pi != pj {
			return pi > pj
		}
		return out[i].Sequence < out[j].Sequence
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *memoryMailboxStore) MarkDelivered(_ context.Context, envelopeID, leaseOwner string, leaseUntil time.Time) (mailbox.Envelope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	env, ok := s.envelopes[envelopeID]
	if !ok {
		return mailbox.Envelope{}, mailbox.ErrNotFound
	}
	if env.State != mailbox.EnvelopePending {
		return mailbox.Envelope{}, fmt.Errorf("%w: envelope %q is in state %q", mailbox.ErrConflict, envelopeID, env.State)
	}
	env.State = mailbox.EnvelopeDelivered
	env.DeliveredAt = time.Now().UTC()
	env.LeaseOwner = leaseOwner
	env.LeaseUntil = leaseUntil
	env.Version++
	return *env, nil
}

func (s *memoryMailboxStore) MarkAcked(_ context.Context, envelopeID, agentID string, ackedAt time.Time, outcome string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	env, ok := s.envelopes[envelopeID]
	if !ok {
		return mailbox.ErrNotFound
	}
	if env.To.AgentID != "" && env.To.AgentID != agentID {
		return fmt.Errorf("%w: ack by %q does not own envelope", mailbox.ErrConflict, agentID)
	}
	if env.State == mailbox.EnvelopeAcked {
		return nil
	}
	env.State = mailbox.EnvelopeAcked
	env.AckedAt = ackedAt
	if outcome != "" {
		if env.Letter.Headers == nil {
			env.Letter.Headers = map[string]string{}
		}
		env.Letter.Headers["mailbox.ackOutcome"] = outcome
	}
	env.LeaseOwner = ""
	env.LeaseUntil = time.Time{}
	env.Version++
	return nil
}

func (s *memoryMailboxStore) MarkDead(_ context.Context, envelopeID, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	env, ok := s.envelopes[envelopeID]
	if !ok {
		return mailbox.ErrNotFound
	}
	env.State = mailbox.EnvelopeDead
	env.DeadReason = reason
	env.LeaseOwner = ""
	env.LeaseUntil = time.Time{}
	env.Version++
	return nil
}

func (s *memoryMailboxStore) IncrementAttempt(_ context.Context, envelopeID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	env, ok := s.envelopes[envelopeID]
	if !ok {
		return 0, mailbox.ErrNotFound
	}
	env.Attempts++
	env.State = mailbox.EnvelopePending
	env.LeaseOwner = ""
	env.LeaseUntil = time.Time{}
	env.Version++
	return env.Attempts, nil
}

func (s *memoryMailboxStore) RecoverExpired(_ context.Context, now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	recovered := 0
	for _, env := range s.envelopes {
		switch env.State {
		case mailbox.EnvelopeDelivered:
			if !env.LeaseUntil.IsZero() && !env.LeaseUntil.After(now) {
				env.State = mailbox.EnvelopePending
				env.LeaseOwner = ""
				env.LeaseUntil = time.Time{}
				env.Version++
				recovered++
			}
		case mailbox.EnvelopePending:
			if !env.ExpiresAt.IsZero() && !env.ExpiresAt.After(now) {
				env.State = mailbox.EnvelopeExpired
				env.Version++
				recovered++
			}
		}
	}
	return recovered, nil
}

func (s *memoryMailboxStore) ListByCorrelation(_ context.Context, teamRunID, correlationID string) ([]mailbox.Envelope, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := s.byCorrelation[correlationKey(teamRunID, correlationID)]
	out := make([]mailbox.Envelope, 0, len(ids))
	for _, id := range ids {
		if env := s.envelopes[id]; env != nil {
			out = append(out, *env)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *memoryMailboxStore) Get(_ context.Context, envelopeID string) (mailbox.Envelope, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	env, ok := s.envelopes[envelopeID]
	if !ok {
		return mailbox.Envelope{}, mailbox.ErrNotFound
	}
	return *env, nil
}

func (s *memoryMailboxStore) CountPending(_ context.Context, teamRunID, agentID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := s.byRecipient[recipientKey(teamRunID, agentID)]
	count := 0
	for _, id := range ids {
		if env := s.envelopes[id]; env != nil && env.State == mailbox.EnvelopePending {
			count++
		}
	}
	return count, nil
}

// priorityRankForEnvelope mirrors mailbox.priorityRank so the store does not
// need to import unexported helpers. Higher = delivered first.
func priorityRankForEnvelope(e mailbox.Envelope) int {
	switch e.Letter.Priority {
	case mailbox.PriorityUrgent:
		return 3
	case mailbox.PriorityHigh:
		return 2
	case mailbox.PriorityLow:
		return 0
	default:
		return 1
	}
}
