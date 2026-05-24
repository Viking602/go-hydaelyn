package api

import (
	"context"
	"time"
)

type RunStore interface {
	SaveRun(context.Context, Run) error
	LoadRun(context.Context, string) (Run, error)
	// ListRuns filters runs by RunSelector. All set selector fields
	// AND-combine. Pagination beyond Limit is provider's choice (cursor /
	// offset / none).
	//
	// Spec anchor: docs/product-spec/v0.8.0/05-storage.md §"RunSelector".
	ListRuns(context.Context, RunSelector) ([]Run, error)
}

type TaskStore interface {
	SaveTask(context.Context, Task) error
	LoadTask(context.Context, string, string) (Task, error)
	ListTasks(context.Context, string) ([]Task, error)
}

type EventStore interface {
	AppendEvent(context.Context, Event) error
	ListEvents(context.Context, string) ([]Event, error)
	// ListAfter returns events with Sequence > afterSeq within the same
	// run, in Sequence order. This is the partial-replay primitive: the
	// runtime calls ListAfter(runID, lastSeenSeq) to resume.
	//
	// Spec anchor: docs/product-spec/v0.8.0/05-storage.md §"Event ordering".
	ListAfter(ctx context.Context, runID string, afterSeq uint64) ([]Event, error)
}

type TraceStore interface {
	SaveTraceSpan(context.Context, TraceSpan) error
	ListTraceSpans(context.Context, string) ([]TraceSpan, error)
}

type TraceSpanUpdater interface {
	LoadTraceSpan(context.Context, string) (TraceSpan, error)
	UpdateTraceSpan(context.Context, TraceSpan) error
}

type BlackboardReadWriter interface {
	WriteItem(context.Context, BlackboardItem) error
	SelectItems(context.Context, string, BlackboardSelector) ([]BlackboardItem, error)
}

type BlackboardCommittedReader interface {
	SelectItems(context.Context, string, BlackboardSelector) ([]BlackboardItem, error)
}

type BlackboardSubscriber interface {
	Subscribe(context.Context, string, BlackboardSelector) (<-chan BlackboardItem, func() error, error)
}

type BlackboardWaiter interface {
	WaitForBlackboard(context.Context, string, BlackboardSelector, func([]BlackboardItem) bool, time.Duration) ([]BlackboardItem, error)
}

type UserMessageStore interface {
	QueueMessage(context.Context, UserMessage) error
	LoadMessage(context.Context, string, string) (UserMessage, error)
	UpdateMessage(context.Context, UserMessage) error
	ListMessages(context.Context, string) ([]UserMessage, error)
	// ListPendingFor restricts to one recipient / run; FIFO within the
	// returned slice. Messages queued in order T1 < T2 MUST appear in
	// the same order.
	//
	// Spec anchor: docs/product-spec/v0.8.0/05-storage.md §"Outbox FIFO".
	ListPendingFor(context.Context, UserMessageSelector) ([]UserMessage, error)
}

type UserMessageOutboxScanner interface {
	ListQueuedMessages(context.Context) ([]UserMessage, error)
}

type MailboxOutboxStore interface {
	QueueEnvelope(context.Context, TaskEnvelope) error
	LoadEnvelope(context.Context, string) (TaskEnvelope, error)
	UpdateEnvelope(context.Context, TaskEnvelope) error
	ListEnvelopes(context.Context, string) ([]TaskEnvelope, error)
}

type LeaseStore interface {
	SaveLease(context.Context, TaskExecutionLease) error
	LoadLease(context.Context, string) (TaskExecutionLease, error)
	ActiveLeaseForTask(context.Context, string, string) (TaskExecutionLease, bool, error)
	// AcquireWithExpectedVersion atomically persists `lease` if and only if
	// the currently-stored lease for the same ID has Version ==
	// expectedVersion. Returns (true, nil) on successful acquire;
	// (false, nil) on version mismatch. MUST be atomic — no observable
	// intermediate state where two acquires both think they succeeded.
	//
	// Spec anchor: docs/product-spec/v0.8.0/05-storage.md §"Lease CAS — the
	// critical contract".
	AcquireWithExpectedVersion(ctx context.Context, lease TaskExecutionLease, expectedVersion uint64) (bool, error)
	// ExtendLease atomically advances the lease Expiry if and only if the
	// current holder equals workerID. Returns (false, nil) if the lease
	// has rotated to a different holder, expired, or does not exist.
	ExtendLease(ctx context.Context, leaseID string, workerID string, newExpiry time.Time) (bool, error)
}

type ApprovalStore interface {
	SaveApproval(context.Context, ApprovalRequest) error
	LoadApproval(context.Context, string) (ApprovalRequest, error)
}

type ResumeTokenStore interface {
	SaveResumeToken(context.Context, ResumeToken) error
	LoadResumeToken(context.Context, string) (ResumeToken, error)
	// ListPending returns resume tokens that have not yet been consumed.
	// Providers that report SupportsListPending=false MAY return an empty
	// slice and the runtime falls back to a polling path.
	//
	// Spec anchor: docs/product-spec/v0.8.0/05-storage.md §"Resume token
	// enumeration".
	ListPending(context.Context, ResumeTokenSelector) ([]ResumeToken, error)
}

type ActionAttemptStore interface {
	SaveActionAttempt(context.Context, ActionAttempt) error
	LoadActionAttempt(context.Context, string) (ActionAttempt, error)
}

// AgentProfileStore persists the framework-level identity of agents.
// AgentProfile.ID is the natural key; Save MUST be idempotent on ID.
//
// Spec anchor: docs/product-spec/v0.8.0/03-agent-ontology.md.
type AgentProfileStore interface {
	SaveAgentProfile(context.Context, AgentProfile) error
	LoadAgentProfile(context.Context, string) (AgentProfile, error)
	ListAgentProfiles(context.Context, AgentSelector) ([]AgentProfile, error)
}

// CapabilityStore persists Capability declarations. Capability.Name +
// Capability.AgentID is the natural key.
//
// Spec anchor: docs/product-spec/v0.8.0/03-agent-ontology.md.
type CapabilityStore interface {
	SaveCapability(context.Context, Capability) error
	LoadCapability(ctx context.Context, name string, agentID string) (Capability, error)
	ListCapabilities(context.Context, CapabilitySelector) ([]Capability, error)
}

// UsageStore is the append-only metering ledger. Records are never
// updated or deleted; queries roll up by selector. SumCredits returns
// the sum of UsageRecord.Credits over matching records.
//
// Spec anchor: docs/product-spec/v0.8.0/06-usage-metering.md.
type UsageStore interface {
	AppendUsage(context.Context, UsageRecord) error
	QueryUsage(context.Context, UsageSelector) ([]UsageRecord, error)
	SumCredits(context.Context, UsageSelector) (int64, error)
}

// DeadLetterStore captures envelopes that exhausted retries. Requeue is
// OPTIONAL and gated on StoreCapabilities.SupportsDeadLetterRequeue;
// providers that report false MAY return an error from Requeue.
//
// Spec anchor: docs/product-spec/v0.8.0/04-worker-runtime.md.
type DeadLetterStore interface {
	AppendDeadLetter(context.Context, DeadLetterEntry) error
	ListDeadLetters(context.Context, DeadLetterSelector) ([]DeadLetterEntry, error)
	Requeue(ctx context.Context, deadLetterID string) error
}

type UnitOfWork interface {
	Runs() RunStore
	Tasks() TaskStore
	Events() EventStore
	Blackboard() BlackboardReadWriter
	MailboxOutbox() MailboxOutboxStore
	UserMessages() UserMessageStore
	Trace() TraceStore
	Leases() LeaseStore
	Approvals() ApprovalStore
	ResumeTokens() ResumeTokenStore
	ActionAttempts() ActionAttemptStore
	AgentProfiles() AgentProfileStore
	CapabilityCatalog() CapabilityStore
	UsageRecords() UsageStore
	DeadLetters() DeadLetterStore
	Commit(context.Context) error
	Rollback(context.Context) error
}

type StoreProvider interface {
	Begin(context.Context) (UnitOfWork, error)
}

// StoreCapabilities is the provider's self-declaration of optional features.
// The runtime branches on these flags rather than probing the provider.
// A provider that returns false for an optional capability is valid; the
// runtime falls back to a polling / single-writer / non-requeue path.
//
// Providers expose this struct via the optional CapabilityReporter
// interface. Providers that do not implement CapabilityReporter are treated
// as having DefaultStoreCapabilities (the conservative profile).
type StoreCapabilities struct {
	SupportsTransactions        bool `json:"supportsTransactions"`        // UoW commit/rollback are atomic
	SupportsBlackboardSubscribe bool `json:"supportsBlackboardSubscribe"` // can push Blackboard items without polling
	SupportsListPending         bool `json:"supportsListPending"`         // ResumeTokens / Outbox enumeration
	SupportsConcurrentWriters   bool `json:"supportsConcurrentWriters"`   // safe under N>1 worker processes
	SupportsDeadLetterRequeue   bool `json:"supportsDeadLetterRequeue"`   // dead-lettered envelopes can be re-queued
}

// DefaultStoreCapabilities is the conservative profile applied to providers
// that do not implement CapabilityReporter. Single-writer, polling-only, no
// dead-letter requeue. Safe for any provider — the runtime falls back to
// polling and serializes worker access.
func DefaultStoreCapabilities() StoreCapabilities {
	return StoreCapabilities{
		SupportsTransactions:        true, // every provider commits or rolls back
		SupportsBlackboardSubscribe: false,
		SupportsListPending:         false,
		SupportsConcurrentWriters:   false,
		SupportsDeadLetterRequeue:   false,
	}
}

// CapabilityReporter is an OPTIONAL StoreProvider extension. Providers that
// implement this interface report which optional features they support so
// the runtime can branch deterministically rather than probing. Providers
// that do not implement this interface are treated as having
// DefaultStoreCapabilities.
//
// Spec anchor: docs/product-spec/v0.8.0/05-storage.md §"Capability reporting".
type CapabilityReporter interface {
	Capabilities(ctx context.Context) (StoreCapabilities, error)
}

// ProviderCloser is an OPTIONAL StoreProvider extension. Providers that
// implement this interface receive a clean shutdown hook from the runtime.
// Providers that do not implement this interface are simply abandoned —
// callers may still close pools or connections via provider-specific APIs.
//
// Spec anchor: docs/product-spec/v0.8.0/05-storage.md §"Top-level provider".
type ProviderCloser interface {
	Close(ctx context.Context) error
}

// LeaseCAS is kept as a documentary alias of the lease CAS contract; the
// underlying methods are now REQUIRED on LeaseStore. External code that
// needs to identify the CAS contract specifically can use this alias for
// readability — but every LeaseStore implementation already satisfies it.
//
// Spec anchor: docs/product-spec/v0.8.0/05-storage.md §"Lease CAS — the
// critical contract".
type LeaseCAS interface {
	AcquireWithExpectedVersion(ctx context.Context, lease TaskExecutionLease, expectedVersion uint64) (bool, error)
	ExtendLease(ctx context.Context, leaseID string, workerID string, newExpiry time.Time) (bool, error)
}
