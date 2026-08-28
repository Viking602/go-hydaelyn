package core

import (
	"github.com/Viking602/venat/api"
	blackboardsvc "github.com/Viking602/venat/internal/blackboard"
	"github.com/Viking602/venat/internal/core/ports"
)

type (
	RunStore                       = ports.RunStore
	TaskStore                      = ports.TaskStore
	EventStore                     = ports.EventStore
	TraceStore                     = ports.TraceStore
	BlackboardReadWriter           = ports.BlackboardReadWriter
	BlackboardSubscriber           = ports.BlackboardSubscriber
	UserMessageStore               = ports.UserMessageStore
	MailboxOutboxStore             = ports.MailboxOutboxStore
	LeaseStore                     = ports.LeaseStore
	ApprovalStore                  = ports.ApprovalStore
	ResumeTokenStore               = ports.ResumeTokenStore
	ActionAttemptStore             = ports.ActionAttemptStore
	AgentProfileStore              = ports.AgentProfileStore
	CapabilityStore                = ports.CapabilityStore
	UsageStore                     = ports.UsageStore
	DeadLetterStore                = ports.DeadLetterStore
	HandoffStore                   = ports.HandoffStore
	TeamStateStore                 = ports.TeamStateStore
	AgentInstanceStore             = ports.AgentInstanceStore
	AgentDefinitionStore           = ports.AgentDefinitionStore
	AgentDefinitionUnitOfWork      = ports.AgentDefinitionUnitOfWork
	AdmissionReservationStore      = ports.AdmissionReservationStore
	AdmissionReservationUnitOfWork = ports.AdmissionReservationUnitOfWork
	ResourceClaimStore             = ports.ResourceClaimStore
	ResourceClaimUnitOfWork        = ports.ResourceClaimUnitOfWork
	BlackboardStore                = ports.BlackboardReadWriter
	UnitOfWork                     = ports.UnitOfWork
	StoreProvider                  = ports.StoreProvider
	CapabilityReporter             = ports.CapabilityReporter
	ProviderCloser                 = ports.ProviderCloser
)

type RuntimeCommand = api.Command

type WriteBlackboardItemCommand = blackboardsvc.WriteItemCommand

type (
	PolicyEngine             = ports.PolicyEngine
	PolicyObligationEnforcer = ports.PolicyObligationEnforcer
	OutputGateway            = ports.OutputGateway
)

type (
	IntentAnalyzer     = ports.IntentAnalyzer
	Planner            = ports.Planner
	PlanValidator      = ports.PlanValidator
	TaskRouter         = ports.TaskRouter
	Dispatcher         = ports.Dispatcher
	TaskMonitor        = ports.TaskMonitor
	PipelineComponents = ports.PipelineComponents
)

type MailboxOutbox = api.TaskEnvelope
