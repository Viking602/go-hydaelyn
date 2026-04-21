package storage

import (
	"context"
	"time"

	"github.com/Viking602/go-hydaelyn/mailbox"
)

// MailboxStore is the persistence surface behind a mailbox.Mailbox
// implementation. It is intentionally stateless with respect to in-memory
// leases — leases are expressed through MarkDelivered(leaseOwner, leaseUntil)
// so a distributed backend can enforce them via conditional updates.
type MailboxStore interface {
	Append(ctx context.Context, env mailbox.Envelope) (mailbox.Envelope, error)
	ListPending(ctx context.Context, teamRunID, agentID string, limit int) ([]mailbox.Envelope, error)
	NextSequence(ctx context.Context, teamRunID, agentID string) (int64, error)
	MarkDelivered(ctx context.Context, envelopeID, leaseOwner string, leaseUntil time.Time) (mailbox.Envelope, error)
	MarkAcked(ctx context.Context, envelopeID, agentID string, ackedAt time.Time, outcome string) error
	MarkDead(ctx context.Context, envelopeID, reason string) error
	IncrementAttempt(ctx context.Context, envelopeID string) (int, error)
	RecoverExpired(ctx context.Context, now time.Time) (int, error)
	ListByCorrelation(ctx context.Context, teamRunID, correlationID string) ([]mailbox.Envelope, error)
	Get(ctx context.Context, envelopeID string) (mailbox.Envelope, error)
	CountPending(ctx context.Context, teamRunID, agentID string) (int, error)
}

// NoopMailboxStore returns a MailboxStore that fails every call with
// mailbox.ErrMailboxUnavailable. External storage drivers that do not yet
// implement Mailboxes() can return this stub.
func NoopMailboxStore() MailboxStore {
	return noopMailboxStore{}
}

type noopMailboxStore struct{}

func (noopMailboxStore) Append(context.Context, mailbox.Envelope) (mailbox.Envelope, error) {
	return mailbox.Envelope{}, mailbox.ErrMailboxUnavailable
}
func (noopMailboxStore) ListPending(context.Context, string, string, int) ([]mailbox.Envelope, error) {
	return nil, mailbox.ErrMailboxUnavailable
}
func (noopMailboxStore) NextSequence(context.Context, string, string) (int64, error) {
	return 0, mailbox.ErrMailboxUnavailable
}
func (noopMailboxStore) MarkDelivered(context.Context, string, string, time.Time) (mailbox.Envelope, error) {
	return mailbox.Envelope{}, mailbox.ErrMailboxUnavailable
}
func (noopMailboxStore) MarkAcked(context.Context, string, string, time.Time, string) error {
	return mailbox.ErrMailboxUnavailable
}
func (noopMailboxStore) MarkDead(context.Context, string, string) error {
	return mailbox.ErrMailboxUnavailable
}
func (noopMailboxStore) IncrementAttempt(context.Context, string) (int, error) {
	return 0, mailbox.ErrMailboxUnavailable
}
func (noopMailboxStore) RecoverExpired(context.Context, time.Time) (int, error) {
	return 0, mailbox.ErrMailboxUnavailable
}
func (noopMailboxStore) ListByCorrelation(context.Context, string, string) ([]mailbox.Envelope, error) {
	return nil, mailbox.ErrMailboxUnavailable
}
func (noopMailboxStore) Get(context.Context, string) (mailbox.Envelope, error) {
	return mailbox.Envelope{}, mailbox.ErrNotFound
}
func (noopMailboxStore) CountPending(context.Context, string, string) (int, error) {
	return 0, mailbox.ErrMailboxUnavailable
}
