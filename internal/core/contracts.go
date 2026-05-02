package core

import (
	"context"

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
	BlackboardStore           = ports.BlackboardReadWriter
	UnitOfWork                = ports.UnitOfWork
	StoreProvider             = ports.StoreProvider
)

type RuntimeCommand interface {
	CommandName() string
}

type WriteBlackboardItemCommand struct {
	Item BlackboardItem
}

type (
	PolicyEngine  = ports.PolicyEngine
	OutputGateway = ports.OutputGateway
)

type UserTimelineProjector interface {
	ProjectUserTimeline(context.Context, []Event) ([]RunTimelineItem, error)
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

type MailboxOutbox = TaskEnvelope
