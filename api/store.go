package api

import (
	"context"
	"time"
)

type RunStore interface {
	SaveRun(context.Context, Run) error
	LoadRun(context.Context, string) (Run, error)
}

type TaskStore interface {
	SaveTask(context.Context, Task) error
	LoadTask(context.Context, string, string) (Task, error)
	ListTasks(context.Context, string) ([]Task, error)
}

type EventStore interface {
	AppendEvent(context.Context, Event) error
	ListEvents(context.Context, string) ([]Event, error)
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
}

type ApprovalStore interface {
	SaveApproval(context.Context, ApprovalRequest) error
	LoadApproval(context.Context, string) (ApprovalRequest, error)
}

type ResumeTokenStore interface {
	SaveResumeToken(context.Context, ResumeToken) error
	LoadResumeToken(context.Context, string) (ResumeToken, error)
}

type ActionAttemptStore interface {
	SaveActionAttempt(context.Context, ActionAttempt) error
	LoadActionAttempt(context.Context, string) (ActionAttempt, error)
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
