package ports

import (
	"context"
	"time"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

type RunStore interface {
	SaveRun(context.Context, model.Run) error
	LoadRun(context.Context, string) (model.Run, error)
	ListRuns(context.Context, model.RunSelector) ([]model.Run, error)
}

type TaskStore interface {
	SaveTask(context.Context, model.Task) error
	LoadTask(context.Context, string, string) (model.Task, error)
	ListTasks(context.Context, string) ([]model.Task, error)
}

type EventStore interface {
	AppendEvent(context.Context, model.Event) error
	ListEvents(context.Context, string) ([]model.Event, error)
	ListAfter(ctx context.Context, runID string, afterSeq uint64) ([]model.Event, error)
}

type TraceStore interface {
	SaveTraceSpan(context.Context, model.TraceSpan) error
	ListTraceSpans(context.Context, string) ([]model.TraceSpan, error)
}

type TraceSpanUpdater interface {
	LoadTraceSpan(context.Context, string) (model.TraceSpan, error)
	UpdateTraceSpan(context.Context, model.TraceSpan) error
}

type BlackboardReadWriter interface {
	WriteItem(context.Context, model.BlackboardItem) error
	SelectItems(context.Context, string, model.BlackboardSelector) ([]model.BlackboardItem, error)
}

type BlackboardCommittedReader interface {
	SelectItems(context.Context, string, model.BlackboardSelector) ([]model.BlackboardItem, error)
}

type BlackboardSubscriber interface {
	Subscribe(context.Context, string, model.BlackboardSelector) (<-chan model.BlackboardItem, func() error, error)
}

type BlackboardWaiter interface {
	WaitForBlackboard(context.Context, string, model.BlackboardSelector, func([]model.BlackboardItem) bool, time.Duration) ([]model.BlackboardItem, error)
}

type UserMessageStore interface {
	QueueMessage(context.Context, model.UserMessage) error
	LoadMessage(context.Context, string, string) (model.UserMessage, error)
	UpdateMessage(context.Context, model.UserMessage) error
	ListMessages(context.Context, string) ([]model.UserMessage, error)
	ListPendingFor(context.Context, model.UserMessageSelector) ([]model.UserMessage, error)
}

type UserMessageOutboxScanner interface {
	ListQueuedMessages(context.Context) ([]model.UserMessage, error)
}

type MailboxOutboxStore interface {
	QueueEnvelope(context.Context, model.TaskEnvelope) error
	LoadEnvelope(context.Context, string) (model.TaskEnvelope, error)
	UpdateEnvelope(context.Context, model.TaskEnvelope) error
	ListEnvelopes(context.Context, string) ([]model.TaskEnvelope, error)
}

type LeaseStore interface {
	SaveLease(context.Context, model.TaskExecutionLease) error
	LoadLease(context.Context, string) (model.TaskExecutionLease, error)
	ActiveLeaseForTask(context.Context, string, string) (model.TaskExecutionLease, bool, error)
	AcquireWithExpectedVersion(ctx context.Context, lease model.TaskExecutionLease, expectedVersion uint64) (bool, error)
	ExtendLease(ctx context.Context, leaseID string, workerID string, newExpiry time.Time) (bool, error)
}

type ApprovalStore interface {
	SaveApproval(context.Context, model.ApprovalRequest) error
	LoadApproval(context.Context, string) (model.ApprovalRequest, error)
}

type ResumeTokenStore interface {
	SaveResumeToken(context.Context, model.ResumeToken) error
	LoadResumeToken(context.Context, string) (model.ResumeToken, error)
	ListPending(context.Context, model.ResumeTokenSelector) ([]model.ResumeToken, error)
}

type ActionAttemptStore interface {
	SaveActionAttempt(context.Context, model.ActionAttempt) error
	LoadActionAttempt(context.Context, string) (model.ActionAttempt, error)
	LoadActionAttemptByIdempotencyKey(ctx context.Context, runID string, taskID string, toolName string, key string) (model.ActionAttempt, error)
}

type AgentProfileStore interface {
	SaveAgentProfile(context.Context, model.AgentProfile) error
	LoadAgentProfile(context.Context, string) (model.AgentProfile, error)
	ListAgentProfiles(context.Context, model.AgentSelector) ([]model.AgentProfile, error)
}

type CapabilityStore interface {
	SaveCapability(context.Context, model.Capability) error
	LoadCapability(ctx context.Context, name string, agentID string) (model.Capability, error)
	ListCapabilities(context.Context, model.CapabilitySelector) ([]model.Capability, error)
}

type UsageStore interface {
	AppendUsage(context.Context, model.UsageRecord) error
	QueryUsage(context.Context, model.UsageSelector) ([]model.UsageRecord, error)
	SumCredits(context.Context, model.UsageSelector) (int64, error)
}

type DeadLetterStore interface {
	AppendDeadLetter(context.Context, model.DeadLetterEntry) error
	ListDeadLetters(context.Context, model.DeadLetterSelector) ([]model.DeadLetterEntry, error)
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

// StoreCapabilities mirrors api.StoreCapabilities on the internal side.
// Kept in lockstep with the public type — see api/store.go for the
// authoritative godoc.
type StoreCapabilities struct {
	SupportsTransactions        bool
	SupportsBlackboardSubscribe bool
	SupportsListPending         bool
	SupportsConcurrentWriters   bool
	SupportsDeadLetterRequeue   bool
}

// DefaultStoreCapabilities returns the conservative profile applied to
// providers that do not implement CapabilityReporter.
func DefaultStoreCapabilities() StoreCapabilities {
	return StoreCapabilities{
		SupportsTransactions:        true,
		SupportsBlackboardSubscribe: false,
		SupportsListPending:         false,
		SupportsConcurrentWriters:   false,
		SupportsDeadLetterRequeue:   false,
	}
}

// CapabilityReporter mirrors api.CapabilityReporter on the internal side.
type CapabilityReporter interface {
	Capabilities(ctx context.Context) (StoreCapabilities, error)
}

// ProviderCloser mirrors api.ProviderCloser on the internal side.
type ProviderCloser interface {
	Close(ctx context.Context) error
}

// LeaseCAS mirrors api.LeaseCAS on the internal side. Atomic acquire +
// conditional renewal — see api/store.go for full contract.
type LeaseCAS interface {
	AcquireWithExpectedVersion(ctx context.Context, lease model.TaskExecutionLease, expectedVersion uint64) (bool, error)
	ExtendLease(ctx context.Context, leaseID string, workerID string, newExpiry time.Time) (bool, error)
}
