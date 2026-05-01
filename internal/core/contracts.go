package core

import (
	"context"

	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

type RunStore = ports.RunStore
type TaskStore = ports.TaskStore
type EventStore = ports.EventStore
type TraceStore = ports.TraceStore
type BlackboardReadWriter = ports.BlackboardReadWriter
type BlackboardCommittedReader = ports.BlackboardCommittedReader
type BlackboardSubscriber = ports.BlackboardSubscriber
type BlackboardWaiter = ports.BlackboardWaiter
type UserMessageStore = ports.UserMessageStore
type UserMessageOutboxScanner = ports.UserMessageOutboxScanner
type MailboxOutboxStore = ports.MailboxOutboxStore
type LeaseStore = ports.LeaseStore
type ApprovalStore = ports.ApprovalStore
type ResumeTokenStore = ports.ResumeTokenStore
type ActionAttemptStore = ports.ActionAttemptStore
type LeaseAwareUnitOfWork = ports.LeaseAwareUnitOfWork
type ApprovalAwareUnitOfWork = ports.ApprovalAwareUnitOfWork
type ActionAwareUnitOfWork = ports.ActionAwareUnitOfWork
type FullUnitOfWork = ports.FullUnitOfWork
type MissingOptionalStores = ports.MissingOptionalStores
type FallbackTx = ports.FallbackTx
type FallbackProvider = ports.FallbackProvider

// BlackboardStore is the legacy public store surface. New internal code should
// use ports.BlackboardReadWriter plus a separate BlackboardSubscriber.
type BlackboardStore interface {
	WriteItem(context.Context, BlackboardItem) error
	SelectItems(context.Context, string, BlackboardSelector) ([]BlackboardItem, error)
	Subscribe(context.Context, string, BlackboardFilter) (<-chan BlackboardItem, func() error, error)
}

// UnitOfWork is the legacy public contract. It intentionally keeps
// Blackboard() returning BlackboardStore for compatibility with existing
// external durable stores.
type UnitOfWork interface {
	Runs() RunStore
	Tasks() TaskStore
	Events() EventStore
	Blackboard() BlackboardStore
	MailboxOutbox() MailboxOutboxStore
	UserMessages() UserMessageStore
	Trace() TraceStore
	Commit(context.Context) error
	Rollback(context.Context) error
}

type StoreProvider interface {
	Begin(context.Context) (UnitOfWork, error)
}

type RuntimeCommand interface {
	CommandName() string
}

type WriteBlackboardItemCommand struct {
	Item BlackboardItem
}

type PolicyEngine = ports.PolicyEngine
type OutputGateway = ports.OutputGateway

type UserTimelineProjector interface {
	ProjectUserTimeline(context.Context, []Event) ([]RunTimelineItem, error)
}

type Projector = ports.Projector
type IntentAnalyzer = ports.IntentAnalyzer
type Planner = ports.Planner
type PlanValidator = ports.PlanValidator
type TaskRouter = ports.TaskRouter
type Dispatcher = ports.Dispatcher
type TaskMonitor = ports.TaskMonitor
type PipelineComponents = ports.PipelineComponents

type MailboxOutbox = TaskEnvelope
