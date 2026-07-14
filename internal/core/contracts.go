package core

import (
	"context"

	blackboardsvc "github.com/Viking602/go-hydaelyn/internal/blackboard"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

type (
	RunStore                  = ports.RunStore
	TaskStore                 = ports.TaskStore
	EventStore                = ports.EventStore
	TraceStore                = ports.TraceStore
	BlackboardReadWriter      = ports.BlackboardReadWriter
	BlackboardCommittedReader = ports.BlackboardCommittedReader
	BlackboardSubscriber      = ports.BlackboardSubscriber
	BlackboardWaiter          = ports.BlackboardWaiter
	UserMessageStore          = ports.UserMessageStore
	UserMessageOutboxScanner  = ports.UserMessageOutboxScanner
	MailboxOutboxStore        = ports.MailboxOutboxStore
	LeaseStore                = ports.LeaseStore
	ApprovalStore             = ports.ApprovalStore
	ResumeTokenStore          = ports.ResumeTokenStore
	ActionAttemptStore        = ports.ActionAttemptStore
	AgentProfileStore         = ports.AgentProfileStore
	CapabilityStore           = ports.CapabilityStore
	UsageStore                = ports.UsageStore
	DeadLetterStore           = ports.DeadLetterStore
	HandoffStore              = ports.HandoffStore
	TeamStateStore            = ports.TeamStateStore
	AgentInstanceStore        = ports.AgentInstanceStore
	BlackboardStore           = ports.BlackboardReadWriter
	UnitOfWork                = ports.UnitOfWork
	StoreProvider             = ports.StoreProvider
	CapabilityReporter        = ports.CapabilityReporter
	ProviderCloser            = ports.ProviderCloser
)

type RuntimeCommand interface {
	CommandName() string
}

type WriteBlackboardItemCommand = blackboardsvc.WriteItemCommand

type (
	PolicyEngine  = ports.PolicyEngine
	OutputGateway = ports.OutputGateway
)

type UserTimelineProjector interface {
	ProjectUserTimeline(context.Context, []model.Event) ([]model.RunTimelineItem, error)
}

type (
	Projector          = ports.Projector
	IntentAnalyzer     = ports.IntentAnalyzer
	Planner            = ports.Planner
	PlanValidator      = ports.PlanValidator
	TaskRouter         = ports.TaskRouter
	Dispatcher         = ports.Dispatcher
	TaskMonitor        = ports.TaskMonitor
	PipelineComponents = ports.PipelineComponents
)

type MailboxOutbox = model.TaskEnvelope
