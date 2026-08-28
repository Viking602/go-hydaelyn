package ports

import "github.com/Viking602/venat/api"

type (
	RunStore                       = api.RunStore
	TaskStore                      = api.TaskStore
	EventStore                     = api.EventStore
	TraceStore                     = api.TraceStore
	BlackboardReadWriter           = api.BlackboardReadWriter
	BlackboardSubscriber           = api.BlackboardSubscriber
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
)

func DefaultStoreCapabilities() StoreCapabilities {
	return api.DefaultStoreCapabilities()
}
