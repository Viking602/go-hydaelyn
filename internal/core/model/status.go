package model

type RunStatus string

const (
	RunStatusCreated           RunStatus = "created"
	RunStatusPlanning          RunStatus = "planning"
	RunStatusValidating        RunStatus = "validating"
	RunStatusRouting           RunStatus = "routing"
	RunStatusDispatching       RunStatus = "dispatching"
	RunStatusRunning           RunStatus = "running"
	RunStatusWaitingUserInput  RunStatus = "waiting_user_input"
	RunStatusWaitingApproval   RunStatus = "waiting_approval"
	RunStatusExecuting         RunStatus = "executing"
	RunStatusReconcileRequired RunStatus = "reconcile_required"
	RunStatusComposingResponse RunStatus = "composing_response"
	RunStatusCompleted         RunStatus = "completed"
	RunStatusFailed            RunStatus = "failed"
	RunStatusBlocked           RunStatus = "blocked"
	RunStatusCancelled         RunStatus = "cancelled"
)

// TaskType is a free-form label set by the developer when creating a task.
// The framework only guarantees behavior for two reserved values:
//
//   - TaskTypeWorker:   default; ordinary work performed by an agent.
//   - TaskTypeResponse: composes the user-visible response (response.go).
//
// Any other value is opaque to the runtime and may be used for business
// classification (use Task.Tags for cross-cutting categorization). To allow a
// task to drive ActionAttempts, set Task.AllowsAction (not the type).
type TaskType string

const (
	TaskTypeWorker   TaskType = "worker"
	TaskTypeResponse TaskType = "response"
)

// AwaitMode controls how a task's DependsOn list is evaluated. The zero value
// (AwaitModeAll) requires every dependency to complete; AwaitModeAny releases
// after a single completion; AwaitModeQuorum needs at least Task.AwaitQuorum
// completions.
type AwaitMode string

const (
	AwaitModeAll    AwaitMode = ""
	AwaitModeAny    AwaitMode = "any"
	AwaitModeQuorum AwaitMode = "quorum"
)

// OnDependencyFailed governs what happens when one of a task's dependencies
// reaches a terminal failure state (Failed/Cancelled). The zero value
// (OnDependencyFailedContinue) keeps waiting (legacy behaviour); Skip counts
// the failure toward the AwaitMode quota; Fail marks the dependent task as
// fatally blocked so the dispatcher can fail it.
type OnDependencyFailed string

const (
	OnDependencyFailedContinue OnDependencyFailed = ""
	OnDependencyFailedSkip     OnDependencyFailed = "skip"
	OnDependencyFailedFail     OnDependencyFailed = "fail"
)

type TaskStatus string

const (
	TaskStatusCreated           TaskStatus = "created"
	TaskStatusPlanned           TaskStatus = "planned"
	TaskStatusValidated         TaskStatus = "validated"
	TaskStatusRouted            TaskStatus = "routed"
	TaskStatusWaitingDependency TaskStatus = "waiting_dependency"
	TaskStatusDispatched        TaskStatus = "dispatched"
	TaskStatusRunning           TaskStatus = "running"
	TaskStatusPaused            TaskStatus = "paused"
	TaskStatusWaitingUserInput  TaskStatus = "waiting_user_input"
	TaskStatusReconcileRequired TaskStatus = "reconcile_required"
	TaskStatusBlocked           TaskStatus = "blocked"
	TaskStatusCompleted         TaskStatus = "completed"
	TaskStatusFailed            TaskStatus = "failed"
	TaskStatusCancelled         TaskStatus = "cancelled"
)

type HolderType string

const (
	HolderAgent     HolderType = "agent"
	HolderComponent HolderType = "component"
)

type LeaseStatus string

const (
	LeaseStatusActive   LeaseStatus = "active"
	LeaseStatusReleased LeaseStatus = "released"
	LeaseStatusExpired  LeaseStatus = "expired"
)

type ReportStatus string

const (
	ReportStatusSuccess            ReportStatus = "success"
	ReportStatusPartialSuccess     ReportStatus = "partial_success"
	ReportStatusFailed             ReportStatus = "failed"
	ReportStatusBlocked            ReportStatus = "blocked"
	ReportStatusNeedsHandoff       ReportStatus = "needs_handoff"
	ReportStatusNeedsApproval      ReportStatus = "needs_approval"
	ReportStatusNeedsClarification ReportStatus = "needs_clarification"
)

type ActionAttemptStatus string

const (
	ActionAttemptCreated   ActionAttemptStatus = "created"
	ActionAttemptRunning   ActionAttemptStatus = "running"
	ActionAttemptSucceeded ActionAttemptStatus = "succeeded"
	ActionAttemptFailed    ActionAttemptStatus = "failed"
	ActionAttemptTimeout   ActionAttemptStatus = "timeout"
	ActionAttemptUnknown   ActionAttemptStatus = "unknown"
	ActionAttemptCancelled ActionAttemptStatus = "cancelled"
)
