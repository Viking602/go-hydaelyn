package mailbox

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/Viking602/go-hydaelyn/team"
)

// Store is the storage surface a Mailbox implementation needs. It is defined
// in the mailbox package (not in storage) so third-party implementations can
// satisfy it without a storage dependency. storage.MailboxStore embeds this
// contract verbatim.
type Store interface {
	Append(ctx context.Context, env Envelope) (Envelope, error)
	ListPending(ctx context.Context, teamRunID, agentID string, limit int) ([]Envelope, error)
	NextSequence(ctx context.Context, teamRunID, agentID string) (int64, error)
	MarkDelivered(ctx context.Context, envelopeID, leaseOwner string, leaseUntil time.Time) (Envelope, error)
	MarkAcked(ctx context.Context, envelopeID, agentID string, ackedAt time.Time, outcome string) error
	MarkDead(ctx context.Context, envelopeID, reason string) error
	IncrementAttempt(ctx context.Context, envelopeID string) (int, error)
	RecoverExpired(ctx context.Context, now time.Time) (int, error)
	ListByCorrelation(ctx context.Context, teamRunID, correlationID string) ([]Envelope, error)
	Get(ctx context.Context, envelopeID string) (Envelope, error)
	CountPending(ctx context.Context, teamRunID, agentID string) (int, error)
}

// StateLoader returns the live RunState for a given team run so fanout can
// resolve role/group addresses. Returning (RunState{}, false) means the
// team-run is unknown and Send should fail with ErrNoRecipients.
type StateLoader func(ctx context.Context, teamRunID string) (team.RunState, bool)

// SendHook is invoked once per envelope that Send successfully persists.
// It runs synchronously after Append; use it only for observability, not
// side effects that can fail.
type SendHook func(ctx context.Context, env Envelope)

// MemoryMailbox is a Mailbox that layers in-process policy (rate-limit,
// fanout, defaults, hop cap, size guard) on top of a pluggable Store. The
// Store may be in-memory or distributed.
type MemoryMailbox struct {
	store  Store
	loader StateLoader
	owner  string
	limits Limits

	mu           sync.Mutex
	senderWindow map[string][]time.Time // per-sender send timestamps for rate limiting
	onSend       SendHook
}

// NewMemoryMailbox builds a Mailbox over the given Store and state loader.
// owner is used as the lease owner (e.g. the host Runtime's workerID).
func NewMemoryMailbox(store Store, loader StateLoader, owner string, limits Limits) *MemoryMailbox {
	return &MemoryMailbox{
		store:        store,
		loader:       loader,
		owner:        owner,
		limits:       limits.ApplyDefaults(),
		senderWindow: map[string][]time.Time{},
	}
}

// Send validates the input, fans out the address, and appends one envelope
// per recipient to the store.
func (m *MemoryMailbox) Send(ctx context.Context, in SendInput) ([]string, error) {
	if m == nil || m.store == nil {
		return nil, ErrMailboxUnavailable
	}
	if err := validateSenderAddress(in.From); err != nil {
		return nil, err
	}
	if in.TeamRunID == "" {
		in.TeamRunID = in.From.TeamRunID
	}
	if in.To.TeamRunID == "" {
		in.To.TeamRunID = in.TeamRunID
	}
	if in.From.TeamRunID != in.TeamRunID {
		return nil, fmt.Errorf("%w: sender team-run does not match envelope team-run", ErrInvalidAddress)
	}

	letter := normalizeLetter(in.Letter)
	if bodyBytes(letter) > m.limits.MaxBodySize {
		return nil, fmt.Errorf("%w: letter body is %d bytes, max %d", ErrOverSize, bodyBytes(letter), m.limits.MaxBodySize)
	}

	if err := m.applyHopCap(&letter); err != nil {
		return nil, err
	}
	if err := m.checkRateLimit(in.From, time.Now()); err != nil {
		return nil, err
	}

	recipients, err := m.resolveRecipients(ctx, in.TeamRunID, in.To)
	if err != nil {
		return nil, err
	}
	if err := m.ensureRecipientCapacity(ctx, in.TeamRunID, recipients); err != nil {
		return nil, err
	}

	ttl := in.TTL
	if ttl <= 0 {
		ttl = m.limits.DefaultTTL
	}
	if ttl > m.limits.MaxTTL {
		ttl = m.limits.MaxTTL
	}
	maxAttempts := in.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = m.limits.MaxAttempts
	}

	now := time.Now().UTC()
	expires := now.Add(ttl)
	ids := make([]string, 0, len(recipients))
	savedEnvelopes := make([]Envelope, 0, len(recipients))
	for _, agentID := range recipients {
		recipientAddr := Address{
			Kind:      AddressKindAgent,
			TeamRunID: in.TeamRunID,
			AgentID:   agentID,
		}

		env := Envelope{
			TeamRunID:      in.TeamRunID,
			From:           in.From,
			To:             recipientAddr,
			OriginalTo:     in.To,
			Letter:         letter,
			CorrelationID:  in.CorrelationID,
			InReplyTo:      in.InReplyTo,
			IdempotencyKey: in.IdempotencyKey,
			State:          EnvelopePending,
			MaxAttempts:    maxAttempts,
			ExpiresAt:      expires,
			CreatedAt:      now,
		}

		saved, aerr := m.store.Append(ctx, env)
		if aerr != nil {
			m.rollbackAppended(ctx, ids, aerr)
			return nil, aerr
		}
		ids = append(ids, saved.ID)
		savedEnvelopes = append(savedEnvelopes, saved)
	}
	if m.onSend != nil {
		for _, saved := range savedEnvelopes {
			m.onSend(ctx, saved)
		}
	}
	return ids, nil
}

// SetSendHook installs a callback invoked once per successfully persisted
// envelope. Pass nil to clear.
func (m *MemoryMailbox) SetSendHook(hook SendHook) {
	if m == nil {
		return
	}
	m.onSend = hook
}

// Fetch claims up to `limit` pending envelopes for a recipient, transitioning
// them to Delivered with a lease. Callers must Ack or Nack each returned
// envelope.
func (m *MemoryMailbox) Fetch(ctx context.Context, teamRunID, agentID string, limit int, leaseTTL time.Duration) ([]Envelope, error) {
	if m == nil || m.store == nil {
		return nil, ErrMailboxUnavailable
	}
	if limit <= 0 {
		limit = 8
	}
	if leaseTTL <= 0 {
		leaseTTL = 60 * time.Second
	}

	// Sweep expired envelopes and lapsed leases first.
	if _, err := m.store.RecoverExpired(ctx, time.Now().UTC()); err != nil {
		return nil, err
	}

	pending, err := m.store.ListPending(ctx, teamRunID, agentID, limit)
	if err != nil {
		return nil, err
	}
	leaseUntil := time.Now().UTC().Add(leaseTTL)
	out := make([]Envelope, 0, len(pending))
	for _, env := range pending {
		claimed, derr := m.store.MarkDelivered(ctx, env.ID, m.owner, leaseUntil)
		if derr != nil {
			continue
		}
		out = append(out, claimed)
	}
	return out, nil
}

func (m *MemoryMailbox) Ack(ctx context.Context, r Receipt) error {
	if m == nil || m.store == nil {
		return ErrMailboxUnavailable
	}
	ackedAt := r.AckedAt
	if ackedAt.IsZero() {
		ackedAt = time.Now().UTC()
	}
	return m.store.MarkAcked(ctx, r.EnvelopeID, r.AgentID, ackedAt, r.Outcome)
}

func (m *MemoryMailbox) Nack(ctx context.Context, envelopeID, reason string) error {
	if m == nil || m.store == nil {
		return ErrMailboxUnavailable
	}
	env, err := m.store.Get(ctx, envelopeID)
	if err != nil {
		return err
	}
	maxAttempts := env.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = m.limits.MaxAttempts
	}
	attempts, ierr := m.store.IncrementAttempt(ctx, envelopeID)
	if ierr != nil {
		return ierr
	}
	if attempts >= maxAttempts {
		return m.store.MarkDead(ctx, envelopeID, fmt.Sprintf("max attempts reached: %s", reason))
	}
	return nil
}

func (m *MemoryMailbox) Peek(ctx context.Context, teamRunID, agentID string, limit int) ([]Envelope, error) {
	if m == nil || m.store == nil {
		return nil, ErrMailboxUnavailable
	}
	if limit <= 0 {
		limit = 32
	}
	return m.store.ListPending(ctx, teamRunID, agentID, limit)
}

func (m *MemoryMailbox) RecoverExpiredLeases(ctx context.Context, now time.Time) error {
	if m == nil || m.store == nil {
		return ErrMailboxUnavailable
	}
	_, err := m.store.RecoverExpired(ctx, now)
	return err
}

// Subscribe is a Phase 2 surface. Phase 1 returns ErrMailboxUnavailable so
// callers don't silently hang.
func (m *MemoryMailbox) Subscribe(_ context.Context, _, _ string) (<-chan Envelope, func(), error) {
	return nil, nil, ErrMailboxUnavailable
}

// ListByCorrelation exposes the correlation chain so patterns and tests can
// trace ask↔answer threads.
func (m *MemoryMailbox) ListByCorrelation(ctx context.Context, teamRunID, correlationID string) ([]Envelope, error) {
	if m == nil || m.store == nil {
		return nil, ErrMailboxUnavailable
	}
	return m.store.ListByCorrelation(ctx, teamRunID, correlationID)
}

func (m *MemoryMailbox) resolveRecipients(ctx context.Context, teamRunID string, to Address) ([]string, error) {
	if err := validateTargetAddress(to); err != nil {
		return nil, err
	}
	if m.loader == nil {
		return nil, fmt.Errorf("%w: no state loader registered", ErrNoRecipients)
	}
	state, ok := m.loader(ctx, teamRunID)
	if !ok {
		return nil, fmt.Errorf("%w: unknown team run %q", ErrNoRecipients, teamRunID)
	}
	return ResolveRecipients(state, to)
}

func (m *MemoryMailbox) ensureRecipientCapacity(ctx context.Context, teamRunID string, recipients []string) error {
	for _, agentID := range recipients {
		count, err := m.store.CountPending(ctx, teamRunID, agentID)
		if err != nil {
			return err
		}
		if count >= m.limits.MaxPerRecipient {
			return fmt.Errorf("%w: recipient %q has %d pending", ErrMailboxFull, agentID, count)
		}
	}
	return nil
}

func (m *MemoryMailbox) rollbackAppended(ctx context.Context, ids []string, cause error) {
	reason := "fanout aborted"
	if cause != nil {
		reason = "fanout aborted: " + cause.Error()
	}
	for _, id := range ids {
		_ = m.store.MarkDead(ctx, id, reason)
	}
}

func (m *MemoryMailbox) applyHopCap(letter *Letter) error {
	if letter.Headers == nil {
		return nil
	}
	raw, ok := letter.Headers[HeaderHopCount]
	if !ok {
		return nil
	}
	hops, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	if hops >= m.limits.MaxHops {
		return fmt.Errorf("%w: %d hops", ErrHopLimit, hops)
	}
	letter.Headers[HeaderHopCount] = strconv.Itoa(hops + 1)
	return nil
}

func (m *MemoryMailbox) checkRateLimit(from Address, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := from.TeamRunID + "\x00" + from.AgentID
	window := now.Add(-time.Minute)
	bucket := m.senderWindow[key]
	trimmed := bucket[:0]
	for _, ts := range bucket {
		if ts.After(window) {
			trimmed = append(trimmed, ts)
		}
	}
	if len(trimmed) >= m.limits.SendRatePerMinute {
		m.senderWindow[key] = trimmed
		return fmt.Errorf("%w: %d sends in last minute", ErrRateLimited, len(trimmed))
	}
	trimmed = append(trimmed, now)
	m.senderWindow[key] = trimmed
	return nil
}
