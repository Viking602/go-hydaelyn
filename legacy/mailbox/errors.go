package mailbox

import "errors"

var (
	// ErrMailboxFull indicates a recipient's inbox has reached MaxPerRecipient.
	ErrMailboxFull = errors.New("mailbox full")
	// ErrNoRecipients indicates the address fanout resolved to zero agents.
	ErrNoRecipients = errors.New("mailbox: no recipients")
	// ErrExpired indicates the envelope's TTL has elapsed.
	ErrExpired = errors.New("mailbox: envelope expired")
	// ErrConflict is returned when a CAS update fails due to a concurrent write.
	ErrConflict = errors.New("mailbox: envelope conflict")
	// ErrOverSize indicates the letter body exceeds MaxBodySize.
	ErrOverSize = errors.New("mailbox: letter oversize")
	// ErrRateLimited indicates the sender exceeded its per-minute quota.
	ErrRateLimited = errors.New("mailbox: sender rate limited")
	// ErrMailboxUnavailable indicates the runtime has no backing store.
	ErrMailboxUnavailable = errors.New("mailbox: unavailable")
	// ErrInvalidAddress indicates an Address has inconsistent kind/fields.
	ErrInvalidAddress = errors.New("mailbox: invalid address")
	// ErrHopLimit indicates a letter exceeded MaxHops.
	ErrHopLimit = errors.New("mailbox: hop limit exceeded")
	// ErrNotFound indicates no envelope matched the given ID.
	ErrNotFound = errors.New("mailbox: envelope not found")
)
