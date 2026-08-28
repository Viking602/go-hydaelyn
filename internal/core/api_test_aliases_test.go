package core

// API type and constant aliases for package-local tests.
// Test code in package core may use bare names; this file makes them available
// without duplicating public type declarations in each test file.

import (
	"github.com/Viking602/venat/api"
)

type (
	ActionOutcome          = api.ActionOutcome
	ApprovalRequest        = api.ApprovalRequest
	ApprovalDecision       = api.ApprovalDecision
	AwaitMode              = api.AwaitMode
	BlackboardItem         = api.BlackboardItem
	BlackboardItemType     = api.BlackboardItemType
	BlackboardSelector     = api.BlackboardSelector
	BlackboardVisibility   = api.BlackboardVisibility
	Event                  = api.Event
	EventType              = api.EventType
	HandoffRequest         = api.HandoffRequest
	HolderType             = api.HolderType
	Intent                 = api.Intent
	LeaseStatus            = api.LeaseStatus
	ObligationKind         = api.ObligationKind
	PolicyObligationTarget = api.PolicyObligationTarget
	OnDependencyFailed     = api.OnDependencyFailed
	PolicyDecision         = api.PolicyDecision
	PolicyEffect           = api.PolicyEffect
	PolicyObligation       = api.PolicyObligation
	PolicyOperation        = api.PolicyOperation
	PolicyRequest          = api.PolicyRequest
	ReplayMode             = api.ReplayMode
	ReportStatus           = api.ReportStatus
	ResumeToken            = api.ResumeToken
	RetryPolicy            = api.RetryPolicy
	RoutingPlan            = api.RoutingPlan
	Run                    = api.Run
	RunStatus              = api.RunStatus
	RunTimelineItem        = api.RunTimelineItem
	RunTimelineKind        = api.RunTimelineKind
	SourceIdentity         = api.SourceIdentity
	SourceType             = api.SourceType
	Task                   = api.Task
	TaskEnvelope           = api.TaskEnvelope
	TaskExecutionLease     = api.TaskExecutionLease
	TaskMonitorDecision    = api.TaskMonitorDecision
	TaskRoute              = api.TaskRoute
	TaskStatus             = api.TaskStatus
	TaskType               = api.TaskType
	TodoPlan               = api.TodoPlan
	Tool                   = api.Tool
	ToolEffectType         = api.ToolEffectType
	TraceSpan              = api.TraceSpan
	TraceSpanStatus        = api.TraceSpanStatus
	TypedReport            = api.TypedReport
	UserMessage            = api.UserMessage
	UserMessageStatus      = api.UserMessageStatus
	UserMessageType        = api.UserMessageType
	ActionAttempt          = api.ActionAttempt
	ActionAttemptStatus    = api.ActionAttemptStatus
)

const (
	AwaitModeAny    = api.AwaitModeAny
	AwaitModeQuorum = api.AwaitModeQuorum

	BlackboardItemClaim              = api.BlackboardItemClaim
	BlackboardItemEvidence           = api.BlackboardItemEvidence
	BlackboardItemArtifactRef        = api.BlackboardItemArtifactRef
	BlackboardItemContext            = api.BlackboardItemContext
	BlackboardItemFinding            = api.BlackboardItemFinding
	BlackboardItemHandoffContext     = api.BlackboardItemHandoffContext
	BlackboardItemTaskOutput         = api.BlackboardItemTaskOutput
	BlackboardVisibilityAgentVisible = api.BlackboardVisibilityAgentVisible
	BlackboardVisibilityInternal     = api.BlackboardVisibilityInternal

	EventActionReconcileRequired     = api.EventActionReconcileRequired
	EventActionAttemptStarted        = api.EventActionAttemptStarted
	EventActionAttemptUpdated        = api.EventActionAttemptUpdated
	EventApprovalDecided             = api.EventApprovalDecided
	EventApprovalRequested           = api.EventApprovalRequested
	EventBlackboardItemWritten       = api.EventBlackboardItemWritten
	EventEnvelopeAcked               = api.EventEnvelopeAcked
	EventEnvelopeDeadLettered        = api.EventEnvelopeDeadLettered
	EventHandoffApplied              = api.EventHandoffApplied
	EventHandoffEnvelopeQueued       = api.EventHandoffEnvelopeQueued
	EventHandoffRequested            = api.EventHandoffRequested
	EventIntentAnalyzed              = api.EventIntentAnalyzed
	EventMailboxRetryScheduled       = api.EventMailboxRetryScheduled
	EventPlanCreated                 = api.EventPlanCreated
	EventPlanValidated               = api.EventPlanValidated
	EventPolicyObligationFailed      = api.EventPolicyObligationFailed
	EventResponsePublishFailed       = api.EventResponsePublishFailed
	EventResponsePublished           = api.EventResponsePublished
	EventResumeTokenCreated          = api.EventResumeTokenCreated
	EventRoutingPlanCreated          = api.EventRoutingPlanCreated
	EventRunStarted                  = api.EventRunStarted
	EventRunStatusChanged            = api.EventRunStatusChanged
	EventSystemResponseBypassAudited = api.EventSystemResponseBypassAudited
	EventTaskBlocked                 = api.EventTaskBlocked
	EventTaskCompleted               = api.EventTaskCompleted
	EventTaskPartiallyCompleted      = api.EventTaskPartiallyCompleted
	EventTaskCreated                 = api.EventTaskCreated
	EventTaskDispatched              = api.EventTaskDispatched
	EventPolicyDecisionRecorded      = api.EventPolicyDecisionRecorded
	EventTaskExecutionAcquired       = api.EventTaskExecutionAcquired
	EventTaskExecutionHeartbeat      = api.EventTaskExecutionHeartbeat
	EventTaskExecutionReleased       = api.EventTaskExecutionReleased
	EventTaskFailed                  = api.EventTaskFailed
	EventTaskMonitorDecision         = api.EventTaskMonitorDecision
	EventTaskOwnerChanged            = api.EventTaskOwnerChanged
	EventTaskPaused                  = api.EventTaskPaused
	EventTraceSpanEnded              = api.EventTraceSpanEnded
	EventTraceSpanStarted            = api.EventTraceSpanStarted
	EventTypedReportSubmitted        = api.EventTypedReportSubmitted
	EventUserInputSubmitted          = api.EventUserInputSubmitted
	EventUserMessageComposed         = api.EventUserMessageComposed
	EventUserMessagePolicyChecked    = api.EventUserMessagePolicyChecked
	EventUserMessageQueued           = api.EventUserMessageQueued

	HolderAgent     = api.HolderAgent
	HolderComponent = api.HolderComponent

	LeaseStatusActive   = api.LeaseStatusActive
	LeaseStatusExpired  = api.LeaseStatusExpired
	LeaseStatusReleased = api.LeaseStatusReleased

	ObligationHideInternalTrace      = api.ObligationHideInternalTrace
	ObligationMaskToolOutput         = api.ObligationMaskToolOutput
	ObligationRedactFields           = api.ObligationRedactFields
	ObligationRequireHumanApproval   = api.ObligationRequireHumanApproval
	ObligationRestrictHandoffContext = api.ObligationRestrictHandoffContext
	ObligationSelectorOnly           = api.ObligationSelectorOnly

	PolicyTargetBlackboardRead  = api.PolicyTargetBlackboardRead
	PolicyTargetBlackboardWrite = api.PolicyTargetBlackboardWrite
	PolicyTargetToolResult      = api.PolicyTargetToolResult
	PolicyTargetHandoff         = api.PolicyTargetHandoff
	PolicyTargetResponse        = api.PolicyTargetResponse
	PolicyTargetTrace           = api.PolicyTargetTrace

	OnDependencyFailedContinue = api.OnDependencyFailedContinue
	OnDependencyFailedFail     = api.OnDependencyFailedFail
	OnDependencyFailedSkip     = api.OnDependencyFailedSkip

	PolicyEffectAllow           = api.PolicyEffectAllow
	PolicyEffectDeny            = api.PolicyEffectDeny
	PolicyEffectRequireApproval = api.PolicyEffectRequireApproval
	PolicyEffectPause           = api.PolicyEffectPause
	PolicyEffectAbort           = api.PolicyEffectAbort

	PolicyOperationAction          = api.PolicyOperationAction
	PolicyOperationBlackboardRead  = api.PolicyOperationBlackboardRead
	PolicyOperationBlackboardWrite = api.PolicyOperationBlackboardWrite
	PolicyOperationDispatch        = api.PolicyOperationDispatch
	PolicyOperationHandoff         = api.PolicyOperationHandoff
	PolicyOperationResponseCompose = api.PolicyOperationResponseCompose
	PolicyOperationResponsePublish = api.PolicyOperationResponsePublish
	PolicyOperationToolCall        = api.PolicyOperationToolCall
	PolicyOperationTraceRead       = api.PolicyOperationTraceRead

	ReplayModeAudit    = api.ReplayModeAudit
	ReplayModeRecovery = api.ReplayModeRecovery

	ReportStatusBlocked            = api.ReportStatusBlocked
	ReportStatusFailed             = api.ReportStatusFailed
	ReportStatusNeedsApproval      = api.ReportStatusNeedsApproval
	ReportStatusNeedsClarification = api.ReportStatusNeedsClarification
	ReportStatusNeedsHandoff       = api.ReportStatusNeedsHandoff
	ReportStatusPartialSuccess     = api.ReportStatusPartialSuccess
	ReportStatusSuccess            = api.ReportStatusSuccess

	RunStatusBlocked           = api.RunStatusBlocked
	RunStatusCancelled         = api.RunStatusCancelled
	RunStatusCompleted         = api.RunStatusCompleted
	RunStatusComposingResponse = api.RunStatusComposingResponse
	RunStatusCreated           = api.RunStatusCreated
	RunStatusDispatching       = api.RunStatusDispatching
	RunStatusExecuting         = api.RunStatusExecuting
	RunStatusFailed            = api.RunStatusFailed
	RunStatusPlanning          = api.RunStatusPlanning
	RunStatusReconcileRequired = api.RunStatusReconcileRequired
	RunStatusRouting           = api.RunStatusRouting
	RunStatusRunning           = api.RunStatusRunning
	RunStatusValidating        = api.RunStatusValidating
	RunStatusWaitingApproval   = api.RunStatusWaitingApproval
	RunStatusWaitingUserInput  = api.RunStatusWaitingUserInput

	SourceAgent     = api.SourceAgent
	SourceComponent = api.SourceComponent
	SourceSystem    = api.SourceSystem
	SourceTool      = api.SourceTool

	TaskStatusBlocked           = api.TaskStatusBlocked
	TaskStatusCancelled         = api.TaskStatusCancelled
	TaskStatusCompleted         = api.TaskStatusCompleted
	TaskStatusCreated           = api.TaskStatusCreated
	TaskStatusDispatched        = api.TaskStatusDispatched
	TaskStatusFailed            = api.TaskStatusFailed
	TaskStatusPaused            = api.TaskStatusPaused
	TaskStatusPlanned           = api.TaskStatusPlanned
	TaskStatusReconcileRequired = api.TaskStatusReconcileRequired
	TaskStatusRouted            = api.TaskStatusRouted
	TaskStatusRunning           = api.TaskStatusRunning
	TaskStatusValidated         = api.TaskStatusValidated
	TaskStatusWaitingDependency = api.TaskStatusWaitingDependency
	TaskStatusWaitingUserInput  = api.TaskStatusWaitingUserInput

	TaskTypeResponse = api.TaskTypeResponse
	TaskTypeWorker   = api.TaskTypeWorker

	ToolEffectReadOnly           = api.ToolEffectReadOnly
	ToolEffectWrite              = api.ToolEffectWrite
	ToolEffectExternalSideEffect = api.ToolEffectExternalSideEffect

	TraceSpanEnded   = api.TraceSpanEnded
	TraceSpanFailed  = api.TraceSpanFailed
	TraceSpanStarted = api.TraceSpanStarted

	UserMessageComposed   = api.UserMessageComposed
	UserMessageFailed     = api.UserMessageFailed
	UserMessagePublishing = api.UserMessagePublishing
	UserMessagePublished  = api.UserMessagePublished
	UserMessageQueued     = api.UserMessageQueued
	UserMessageCancelled  = api.UserMessageCancelled

	UserMessageTypeClarificationRequest = api.UserMessageTypeClarificationRequest
	UserMessageTypeErrorNotice          = api.UserMessageTypeErrorNotice
	UserMessageTypeExecutionResult      = api.UserMessageTypeExecutionResult
	UserMessageTypeFinalAnswer          = api.UserMessageTypeFinalAnswer
	UserMessageTypeProgressUpdate       = api.UserMessageTypeProgressUpdate
	UserMessageTypeApprovalRequest      = api.UserMessageTypeApprovalRequest
	UserMessageTypeBlockedNotice        = api.UserMessageTypeBlockedNotice

	ActionAttemptCancelled = api.ActionAttemptCancelled
	ActionAttemptCreated   = api.ActionAttemptCreated
	ActionAttemptFailed    = api.ActionAttemptFailed
	ActionAttemptRunning   = api.ActionAttemptRunning
	ActionAttemptSucceeded = api.ActionAttemptSucceeded
	ActionAttemptTimeout   = api.ActionAttemptTimeout
	ActionAttemptUnknown   = api.ActionAttemptUnknown
)
