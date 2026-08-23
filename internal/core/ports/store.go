package ports

import (
	"context"
	"time"

	"github.com/Viking602/venat/api"
)

type (
	RunStore                       = api.RunStore
	TaskStore                      = api.TaskStore
	EventStore                     = api.EventStore
	TraceStore                     = api.TraceStore
	BlackboardReadWriter           = api.BlackboardReadWriter
	UserMessageStore               = api.UserMessageStore
	MailboxOutboxStore             = api.MailboxOutboxStore
	LeaseStore                     = api.LeaseStore
	ApprovalStore                  = api.ApprovalStore
	ResumeTokenStore               = api.ResumeTokenStore
	ActionAttemptStore             = api.ActionAttemptStore
	AgentProfileStore              = api.AgentProfileStore
	CapabilityStore                = api.CapabilityStore
	UsageStore                     = api.UsageStore
	DeadLetterStore                = api.DeadLetterStore
	HandoffStore                   = api.HandoffStore
	TeamStateStore                 = api.TeamStateStore
	AgentInstanceStore             = api.AgentInstanceStore
	AgentDefinitionStore           = api.AgentDefinitionStore
	AdmissionReservationStore      = api.AdmissionReservationStore
	AdmissionReservationUnitOfWork = api.AdmissionReservationUnitOfWork
	ResourceClaimStore             = api.ResourceClaimStore
	ResourceClaimUnitOfWork        = api.ResourceClaimUnitOfWork
	AgentDefinitionUnitOfWork      = api.AgentDefinitionUnitOfWork
	UnitOfWork                     = api.UnitOfWork
	StoreProvider                  = api.StoreProvider
	StoreCapabilities              = api.StoreCapabilities
	CapabilityReporter             = api.CapabilityReporter
	ProviderCloser                 = api.ProviderCloser
	LeaseCAS                       = api.LeaseCAS
)

// TraceSpanUpdater is the optional mutable trace extension used by the runtime.
type TraceSpanUpdater interface {
	LoadTraceSpan(context.Context, string) (api.TraceSpan, error)
	UpdateTraceSpan(context.Context, api.TraceSpan) error
}

// BlackboardCommittedReader reads committed blackboard state outside a write transaction.
type BlackboardCommittedReader interface {
	SelectItems(context.Context, string, api.BlackboardSelector) ([]api.BlackboardItem, error)
}

// BlackboardSubscriber is the optional push subscription extension.
type BlackboardSubscriber interface {
	Subscribe(context.Context, string, api.BlackboardSelector) (<-chan api.BlackboardItem, func() error, error)
}

// BlackboardWaiter is the optional store-native wait extension.
type BlackboardWaiter interface {
	WaitForBlackboard(context.Context, string, api.BlackboardSelector, func([]api.BlackboardItem) bool, time.Duration) ([]api.BlackboardItem, error)
}

// UserMessageOutboxScanner enumerates every queued user message for recovery.
type UserMessageOutboxScanner interface {
	ListQueuedMessages(context.Context) ([]api.UserMessage, error)
}

func DefaultStoreCapabilities() StoreCapabilities {
	return api.DefaultStoreCapabilities()
}
