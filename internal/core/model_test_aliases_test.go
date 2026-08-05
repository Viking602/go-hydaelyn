package core

// model type and const aliases for test files only.
// Test code in package core may use bare names; this file makes them available
// without modifying every test file to add a model import.

import "github.com/Viking602/venat/internal/core/model"

type (
	ActionOutcome          = model.ActionOutcome
	ApprovalRequest        = model.ApprovalRequest
	ApprovalDecision       = model.ApprovalDecision
	AwaitMode              = model.AwaitMode
	BlackboardItem         = model.BlackboardItem
	BlackboardItemType     = model.BlackboardItemType
	BlackboardSelector     = model.BlackboardSelector
	BlackboardVisibility   = model.BlackboardVisibility
	Event                  = model.Event
	EventType              = model.EventType
	Flow                   = model.Flow
	HandoffRequest         = model.HandoffRequest
	HolderType             = model.HolderType
	Intent                 = model.Intent
	LeaseStatus            = model.LeaseStatus
	ObligationKind         = model.ObligationKind
	PolicyObligationTarget = model.PolicyObligationTarget
	OnDependencyFailed     = model.OnDependencyFailed
	PolicyDecision         = model.PolicyDecision
	PolicyEffect           = model.PolicyEffect
	PolicyObligation       = model.PolicyObligation
	PolicyOperation        = model.PolicyOperation
	PolicyRequest          = model.PolicyRequest
	ReplayMode             = model.ReplayMode
	ReportStatus           = model.ReportStatus
	ResumeToken            = model.ResumeToken
	RetryPolicy            = model.RetryPolicy
	RoutingPlan            = model.RoutingPlan
	Run                    = model.Run
	RunStatus              = model.RunStatus
	RunTimelineItem        = model.RunTimelineItem
	RunTimelineKind        = model.RunTimelineKind
	SourceIdentity         = model.SourceIdentity
	SourceType             = model.SourceType
	Task                   = model.Task
	TaskEnvelope           = model.TaskEnvelope
	TaskExecutionLease     = model.TaskExecutionLease
	TaskMonitorDecision    = model.TaskMonitorDecision
	TaskRoute              = model.TaskRoute
	TaskStatus             = model.TaskStatus
	TaskType               = model.TaskType
	TodoPlan               = model.TodoPlan
	Tool                   = model.Tool
	ToolEffectType         = model.ToolEffectType
	TraceSpan              = model.TraceSpan
	TraceSpanStatus        = model.TraceSpanStatus
	TypedReport            = model.TypedReport
	UserMessage            = model.UserMessage
	UserMessageStatus      = model.UserMessageStatus
	UserMessageType        = model.UserMessageType
	ActionAttempt          = model.ActionAttempt
	ActionAttemptStatus    = model.ActionAttemptStatus
)

const (
	AwaitModeAny    = model.AwaitModeAny
	AwaitModeQuorum = model.AwaitModeQuorum

	BlackboardItemClaim              = model.BlackboardItemClaim
	BlackboardItemEvidence           = model.BlackboardItemEvidence
	BlackboardItemArtifactRef        = model.BlackboardItemArtifactRef
	BlackboardItemContext            = model.BlackboardItemContext
	BlackboardItemFinding            = model.BlackboardItemFinding
	BlackboardItemHandoffContext     = model.BlackboardItemHandoffContext
	BlackboardItemTaskOutput         = model.BlackboardItemTaskOutput
	BlackboardVisibilityAgentVisible = model.BlackboardVisibilityAgentVisible
	BlackboardVisibilityInternal     = model.BlackboardVisibilityInternal

	EventActionReconcileRequired     = model.EventActionReconcileRequired
	EventActionAttemptStarted        = model.EventActionAttemptStarted
	EventActionAttemptUpdated        = model.EventActionAttemptUpdated
	EventApprovalDecided             = model.EventApprovalDecided
	EventApprovalRequested           = model.EventApprovalRequested
	EventBlackboardItemWritten       = model.EventBlackboardItemWritten
	EventEnvelopeAcked               = model.EventEnvelopeAcked
	EventEnvelopeDeadLettered        = model.EventEnvelopeDeadLettered
	EventHandoffApplied              = model.EventHandoffApplied
	EventHandoffEnvelopeQueued       = model.EventHandoffEnvelopeQueued
	EventHandoffRequested            = model.EventHandoffRequested
	EventIntentAnalyzed              = model.EventIntentAnalyzed
	EventMailboxRetryScheduled       = model.EventMailboxRetryScheduled
	EventPlanCreated                 = model.EventPlanCreated
	EventPlanValidated               = model.EventPlanValidated
	EventPolicyObligationFailed      = model.EventPolicyObligationFailed
	EventResponsePublishFailed       = model.EventResponsePublishFailed
	EventResponsePublished           = model.EventResponsePublished
	EventResumeTokenCreated          = model.EventResumeTokenCreated
	EventRoutingPlanCreated          = model.EventRoutingPlanCreated
	EventRunStarted                  = model.EventRunStarted
	EventRunStatusChanged            = model.EventRunStatusChanged
	EventSystemResponseBypassAudited = model.EventSystemResponseBypassAudited
	EventTaskBlocked                 = model.EventTaskBlocked
	EventTaskCompleted               = model.EventTaskCompleted
	EventTaskCreated                 = model.EventTaskCreated
	EventTaskDispatched              = model.EventTaskDispatched
	EventPolicyDecisionRecorded      = model.EventPolicyDecisionRecorded
	EventTaskExecutionAcquired       = model.EventTaskExecutionAcquired
	EventTaskExecutionHeartbeat      = model.EventTaskExecutionHeartbeat
	EventTaskExecutionReleased       = model.EventTaskExecutionReleased
	EventTaskFailed                  = model.EventTaskFailed
	EventTaskMonitorDecision         = model.EventTaskMonitorDecision
	EventTaskOwnerChanged            = model.EventTaskOwnerChanged
	EventTaskPaused                  = model.EventTaskPaused
	EventTraceSpanEnded              = model.EventTraceSpanEnded
	EventTraceSpanStarted            = model.EventTraceSpanStarted
	EventTypedReportSubmitted        = model.EventTypedReportSubmitted
	EventUserInputSubmitted          = model.EventUserInputSubmitted
	EventUserMessageComposed         = model.EventUserMessageComposed
	EventUserMessagePolicyChecked    = model.EventUserMessagePolicyChecked
	EventUserMessageQueued           = model.EventUserMessageQueued

	HolderAgent     = model.HolderAgent
	HolderComponent = model.HolderComponent

	LeaseStatusActive   = model.LeaseStatusActive
	LeaseStatusExpired  = model.LeaseStatusExpired
	LeaseStatusReleased = model.LeaseStatusReleased

	ObligationHideInternalTrace      = model.ObligationHideInternalTrace
	ObligationMaskToolOutput         = model.ObligationMaskToolOutput
	ObligationRedactFields           = model.ObligationRedactFields
	ObligationRequireHumanApproval   = model.ObligationRequireHumanApproval
	ObligationRestrictHandoffContext = model.ObligationRestrictHandoffContext
	ObligationSelectorOnly           = model.ObligationSelectorOnly

	PolicyTargetBlackboardRead  = model.PolicyTargetBlackboardRead
	PolicyTargetBlackboardWrite = model.PolicyTargetBlackboardWrite
	PolicyTargetToolResult      = model.PolicyTargetToolResult
	PolicyTargetHandoff         = model.PolicyTargetHandoff
	PolicyTargetResponse        = model.PolicyTargetResponse
	PolicyTargetTrace           = model.PolicyTargetTrace

	OnDependencyFailedContinue = model.OnDependencyFailedContinue
	OnDependencyFailedFail     = model.OnDependencyFailedFail
	OnDependencyFailedSkip     = model.OnDependencyFailedSkip

	PolicyEffectAllow           = model.PolicyEffectAllow
	PolicyEffectDeny            = model.PolicyEffectDeny
	PolicyEffectRequireApproval = model.PolicyEffectRequireApproval
	PolicyEffectPause           = model.PolicyEffectPause
	PolicyEffectAbort           = model.PolicyEffectAbort

	PolicyOperationAction          = model.PolicyOperationAction
	PolicyOperationBlackboardRead  = model.PolicyOperationBlackboardRead
	PolicyOperationBlackboardWrite = model.PolicyOperationBlackboardWrite
	PolicyOperationDispatch        = model.PolicyOperationDispatch
	PolicyOperationHandoff         = model.PolicyOperationHandoff
	PolicyOperationResponseCompose = model.PolicyOperationResponseCompose
	PolicyOperationResponsePublish = model.PolicyOperationResponsePublish
	PolicyOperationToolCall        = model.PolicyOperationToolCall
	PolicyOperationTraceRead       = model.PolicyOperationTraceRead

	ReplayModeAudit    = model.ReplayModeAudit
	ReplayModeRecovery = model.ReplayModeRecovery

	ReportStatusBlocked            = model.ReportStatusBlocked
	ReportStatusFailed             = model.ReportStatusFailed
	ReportStatusNeedsApproval      = model.ReportStatusNeedsApproval
	ReportStatusNeedsClarification = model.ReportStatusNeedsClarification
	ReportStatusNeedsHandoff       = model.ReportStatusNeedsHandoff
	ReportStatusPartialSuccess     = model.ReportStatusPartialSuccess
	ReportStatusSuccess            = model.ReportStatusSuccess

	RunStatusBlocked           = model.RunStatusBlocked
	RunStatusCancelled         = model.RunStatusCancelled
	RunStatusCompleted         = model.RunStatusCompleted
	RunStatusComposingResponse = model.RunStatusComposingResponse
	RunStatusCreated           = model.RunStatusCreated
	RunStatusDispatching       = model.RunStatusDispatching
	RunStatusExecuting         = model.RunStatusExecuting
	RunStatusFailed            = model.RunStatusFailed
	RunStatusPlanning          = model.RunStatusPlanning
	RunStatusReconcileRequired = model.RunStatusReconcileRequired
	RunStatusRouting           = model.RunStatusRouting
	RunStatusRunning           = model.RunStatusRunning
	RunStatusValidating        = model.RunStatusValidating
	RunStatusWaitingApproval   = model.RunStatusWaitingApproval
	RunStatusWaitingUserInput  = model.RunStatusWaitingUserInput

	SourceAgent     = model.SourceAgent
	SourceComponent = model.SourceComponent
	SourceSystem    = model.SourceSystem
	SourceTool      = model.SourceTool

	TaskStatusBlocked           = model.TaskStatusBlocked
	TaskStatusCancelled         = model.TaskStatusCancelled
	TaskStatusCompleted         = model.TaskStatusCompleted
	TaskStatusCreated           = model.TaskStatusCreated
	TaskStatusDispatched        = model.TaskStatusDispatched
	TaskStatusFailed            = model.TaskStatusFailed
	TaskStatusPaused            = model.TaskStatusPaused
	TaskStatusPlanned           = model.TaskStatusPlanned
	TaskStatusReconcileRequired = model.TaskStatusReconcileRequired
	TaskStatusRouted            = model.TaskStatusRouted
	TaskStatusRunning           = model.TaskStatusRunning
	TaskStatusValidated         = model.TaskStatusValidated
	TaskStatusWaitingDependency = model.TaskStatusWaitingDependency
	TaskStatusWaitingUserInput  = model.TaskStatusWaitingUserInput

	TaskTypeResponse = model.TaskTypeResponse
	TaskTypeWorker   = model.TaskTypeWorker

	ToolEffectReadOnly           = model.ToolEffectReadOnly
	ToolEffectWrite              = model.ToolEffectWrite
	ToolEffectExternalSideEffect = model.ToolEffectExternalSideEffect

	TraceSpanEnded   = model.TraceSpanEnded
	TraceSpanFailed  = model.TraceSpanFailed
	TraceSpanStarted = model.TraceSpanStarted

	UserMessageComposed  = model.UserMessageComposed
	UserMessageFailed    = model.UserMessageFailed
	UserMessagePublished = model.UserMessagePublished
	UserMessageQueued    = model.UserMessageQueued
	UserMessageCancelled = model.UserMessageCancelled

	UserMessageTypeClarificationRequest = model.UserMessageTypeClarificationRequest
	UserMessageTypeErrorNotice          = model.UserMessageTypeErrorNotice
	UserMessageTypeExecutionResult      = model.UserMessageTypeExecutionResult
	UserMessageTypeFinalAnswer          = model.UserMessageTypeFinalAnswer
	UserMessageTypeProgressUpdate       = model.UserMessageTypeProgressUpdate
	UserMessageTypeApprovalRequest      = model.UserMessageTypeApprovalRequest
	UserMessageTypeBlockedNotice        = model.UserMessageTypeBlockedNotice

	ActionAttemptCancelled = model.ActionAttemptCancelled
	ActionAttemptCreated   = model.ActionAttemptCreated
	ActionAttemptFailed    = model.ActionAttemptFailed
	ActionAttemptRunning   = model.ActionAttemptRunning
	ActionAttemptSucceeded = model.ActionAttemptSucceeded
	ActionAttemptTimeout   = model.ActionAttemptTimeout
	ActionAttemptUnknown   = model.ActionAttemptUnknown
)
