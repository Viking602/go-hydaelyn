// Package mailbox implements agent-to-agent signal messaging for hydaelyn.
//
// The blackboard carries shared data; the mailbox carries targeted signals
// between specific agents (ask, answer, delegate, cancel, handoff). Letters
// are persisted through storage.MailboxStore, delivered at-least-once with
// explicit acknowledgement, and addressable by agent id, role, or group.
package mailbox

import (
	"context"
	"time"

	"github.com/Viking602/go-hydaelyn/team"
)

// AddressKind selects how a letter's recipient set is resolved.
type AddressKind string

const (
	// AddressKindAgent targets exactly one agent by AgentID.
	AddressKindAgent AddressKind = "agent"
	// AddressKindRole fans out to every agent in the team-run with the given Role.
	AddressKindRole AddressKind = "role"
	// AddressKindGroup fans out to every agent whose metadata["group"] matches.
	AddressKindGroup AddressKind = "group"
)

// Address identifies a sender or recipient within a team run.
type Address struct {
	Kind      AddressKind `json:"kind"`
	TeamRunID string      `json:"teamRunId"`
	AgentID   string      `json:"agentId,omitempty"`
	Role      team.Role   `json:"role,omitempty"`
	Group     string      `json:"group,omitempty"`
}

// LetterIntent is a coarse tag used by recipients to triage letters.
type LetterIntent string

const (
	IntentAsk       LetterIntent = "ask"
	IntentAnswer    LetterIntent = "answer"
	IntentDelegate  LetterIntent = "delegate"
	IntentCancel    LetterIntent = "cancel"
	IntentBroadcast LetterIntent = "broadcast"
	IntentHandoff   LetterIntent = "handoff"
)

// LetterPriority orders deliveries within a recipient's inbox.
type LetterPriority string

const (
	PriorityLow    LetterPriority = "low"
	PriorityNormal LetterPriority = "normal"
	PriorityHigh   LetterPriority = "high"
	PriorityUrgent LetterPriority = "urgent"
)

// priorityRank maps priority to an integer for ordering. Higher is delivered first.
func priorityRank(p LetterPriority) int {
	switch p {
	case PriorityUrgent:
		return 3
	case PriorityHigh:
		return 2
	case PriorityLow:
		return 0
	default:
		return 1
	}
}

// EnvelopeState tracks the delivery lifecycle of a stored letter.
type EnvelopeState string

const (
	EnvelopePending   EnvelopeState = "pending"
	EnvelopeDelivered EnvelopeState = "delivered"
	EnvelopeAcked     EnvelopeState = "acked"
	EnvelopeExpired   EnvelopeState = "expired"
	EnvelopeDead      EnvelopeState = "dead"
)

// Header keys with defined meaning. Other headers are caller-defined.
const (
	HeaderEphemeral = "mailbox.ephemeral"
	HeaderHopCount  = "mailbox.hopCount"
)

// Letter is the payload sent between agents.
type Letter struct {
	Subject     string            `json:"subject,omitempty"`
	Body        string            `json:"body,omitempty"`
	Structured  map[string]any    `json:"structured,omitempty"`
	ArtifactIDs []string          `json:"artifactIds,omitempty"`
	Intent      LetterIntent      `json:"intent,omitempty"`
	Priority    LetterPriority    `json:"priority,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

// Envelope is the stored, addressed unit of delivery. One envelope per recipient.
type Envelope struct {
	ID             string        `json:"id"`
	TeamRunID      string        `json:"teamRunId"`
	From           Address       `json:"from"`
	To             Address       `json:"to"`
	OriginalTo     Address       `json:"originalTo,omitempty"`
	Letter         Letter        `json:"letter"`
	CorrelationID  string        `json:"correlationId,omitempty"`
	InReplyTo      string        `json:"inReplyTo,omitempty"`
	IdempotencyKey string        `json:"idempotencyKey,omitempty"`
	State          EnvelopeState `json:"state"`
	Sequence       int64         `json:"sequence"`
	Version        int           `json:"version,omitempty"`
	ETag           string        `json:"etag,omitempty"`
	Attempts       int           `json:"attempts,omitempty"`
	MaxAttempts    int           `json:"maxAttempts,omitempty"`
	DeliveredAt    time.Time     `json:"deliveredAt,omitempty"`
	AckedAt        time.Time     `json:"ackedAt,omitempty"`
	ExpiresAt      time.Time     `json:"expiresAt,omitempty"`
	CreatedAt      time.Time     `json:"createdAt"`
	DeadReason     string        `json:"deadReason,omitempty"`
	LeaseOwner     string        `json:"leaseOwner,omitempty"`
	LeaseUntil     time.Time     `json:"leaseUntil,omitempty"`
}

// Receipt acknowledges receipt and processing of an envelope.
type Receipt struct {
	EnvelopeID string    `json:"envelopeId"`
	AgentID    string    `json:"agentId"`
	AckedAt    time.Time `json:"ackedAt"`
	Outcome    string    `json:"outcome,omitempty"`
}

// SendInput is the caller-facing shape for submitting a letter.
type SendInput struct {
	TeamRunID      string
	From           Address
	To             Address
	Letter         Letter
	CorrelationID  string
	InReplyTo      string
	IdempotencyKey string
	TTL            time.Duration
	MaxAttempts    int
}

// Mailbox is the capability an agent uses to send and receive letters.
type Mailbox interface {
	Send(ctx context.Context, in SendInput) ([]string, error)
	Fetch(ctx context.Context, teamRunID, agentID string, limit int, leaseTTL time.Duration) ([]Envelope, error)
	Ack(ctx context.Context, r Receipt) error
	Nack(ctx context.Context, envelopeID, reason string) error
	Peek(ctx context.Context, teamRunID, agentID string, limit int) ([]Envelope, error)
	RecoverExpiredLeases(ctx context.Context, now time.Time) error
	Subscribe(ctx context.Context, teamRunID, agentID string) (<-chan Envelope, func(), error)
}

// Limits controls runtime guardrails applied by a mailbox implementation.
// Zero values fall back to package-level defaults.
type Limits struct {
	MaxBodySize       int
	MaxInlineBodySize int
	MaxPerRecipient   int
	MaxAttempts       int
	MaxHops           int
	DefaultTTL        time.Duration
	MaxTTL            time.Duration
	SendRatePerMinute int
}

// Default limit values. Chosen conservatively; tune via host.Config.
const (
	DefaultMaxBodySize       = 64 * 1024
	DefaultMaxInlineBodySize = 4 * 1024
	DefaultMaxPerRecipient   = 1024
	DefaultMaxAttempts       = 3
	DefaultMaxHops           = 8
	DefaultTTL               = 24 * time.Hour
	DefaultMaxTTL            = 24 * time.Hour
	DefaultSendRatePerMinute = 60
)

// ApplyDefaults fills zero fields from the package defaults.
func (l Limits) ApplyDefaults() Limits {
	if l.MaxBodySize <= 0 {
		l.MaxBodySize = DefaultMaxBodySize
	}
	if l.MaxInlineBodySize <= 0 {
		l.MaxInlineBodySize = DefaultMaxInlineBodySize
	}
	if l.MaxPerRecipient <= 0 {
		l.MaxPerRecipient = DefaultMaxPerRecipient
	}
	if l.MaxAttempts <= 0 {
		l.MaxAttempts = DefaultMaxAttempts
	}
	if l.MaxHops <= 0 {
		l.MaxHops = DefaultMaxHops
	}
	if l.DefaultTTL <= 0 {
		l.DefaultTTL = DefaultTTL
	}
	if l.MaxTTL <= 0 {
		l.MaxTTL = DefaultMaxTTL
	}
	if l.SendRatePerMinute <= 0 {
		l.SendRatePerMinute = DefaultSendRatePerMinute
	}
	return l
}

// IsEphemeral reports whether the letter opts out of durable storage.
func (l Letter) IsEphemeral() bool {
	if l.Headers == nil {
		return false
	}
	return l.Headers[HeaderEphemeral] == "true"
}
