package ports

import (
	"context"
	"time"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

type RunStore interface {
	SaveRun(context.Context, model.Run) error
	LoadRun(context.Context, string) (model.Run, error)
}

type TaskStore interface {
	SaveTask(context.Context, model.Task) error
	LoadTask(context.Context, string, string) (model.Task, error)
	ListTasks(context.Context, string) ([]model.Task, error)
}

type EventStore interface {
	AppendEvent(context.Context, model.Event) error
	ListEvents(context.Context, string) ([]model.Event, error)
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
}

type ApprovalStore interface {
	SaveApproval(context.Context, model.ApprovalRequest) error
	LoadApproval(context.Context, string) (model.ApprovalRequest, error)
}

type ResumeTokenStore interface {
	SaveResumeToken(context.Context, model.ResumeToken) error
	LoadResumeToken(context.Context, string) (model.ResumeToken, error)
}

type ActionAttemptStore interface {
	SaveActionAttempt(context.Context, model.ActionAttempt) error
	LoadActionAttempt(context.Context, string) (model.ActionAttempt, error)
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
	Commit(context.Context) error
	Rollback(context.Context) error
}

type StoreProvider interface {
	Begin(context.Context) (UnitOfWork, error)
}
