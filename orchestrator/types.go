package orchestrator

import runtimeimpl "github.com/Viking602/go-hydaelyn/internal/runtime"

var (
	ErrNotFound                = runtimeimpl.ErrNotFound
	ErrTerminalState           = runtimeimpl.ErrTerminalState
	ErrStaleTaskVersion        = runtimeimpl.ErrStaleTaskVersion
	ErrLeaseHolderMismatch     = runtimeimpl.ErrLeaseHolderMismatch
	ErrLeaseNotActive          = runtimeimpl.ErrLeaseNotActive
	ErrOwnerMismatch           = runtimeimpl.ErrOwnerMismatch
	ErrActionTaskRequired      = runtimeimpl.ErrActionTaskRequired
	ErrActionReconcileRequired = runtimeimpl.ErrActionReconcileRequired
	ErrResponseTaskRequired    = runtimeimpl.ErrResponseTaskRequired
	ErrPolicyDenied            = runtimeimpl.ErrPolicyDenied
	ErrPolicyObligationFailed  = runtimeimpl.ErrPolicyObligationFailed
	ErrFlowBypass              = runtimeimpl.ErrFlowBypass
	ErrHandoffCycle            = runtimeimpl.ErrHandoffCycle
	ErrHandoffDepthExceeded    = runtimeimpl.ErrHandoffDepthExceeded
	ErrInvalidCommand          = runtimeimpl.ErrInvalidCommand
	ErrInvalidTransition       = runtimeimpl.ErrInvalidTransition
	ErrCompletionCriteriaUnmet = runtimeimpl.ErrCompletionCriteriaUnmet
	ErrDependencyUnmet         = runtimeimpl.ErrDependencyUnmet
	ErrDependencyFailed        = runtimeimpl.ErrDependencyFailed
	ErrInvalidAddress          = runtimeimpl.ErrInvalidAddress
	ErrNoRecipients            = runtimeimpl.ErrNoRecipients
	ErrSubscriptionClosed      = runtimeimpl.ErrSubscriptionClosed
	ErrWaitTimeout             = runtimeimpl.ErrWaitTimeout
)

type (
	RunStatus            = runtimeimpl.RunStatus
	TaskType             = runtimeimpl.TaskType
	TaskStatus           = runtimeimpl.TaskStatus
	HolderType           = runtimeimpl.HolderType
	LeaseStatus          = runtimeimpl.LeaseStatus
	ReportStatus         = runtimeimpl.ReportStatus
	ActionAttemptStatus  = runtimeimpl.ActionAttemptStatus
	ToolEffectType       = runtimeimpl.ToolEffectType
	BlackboardVisibility = runtimeimpl.BlackboardVisibility
	BlackboardItemType   = runtimeimpl.BlackboardItemType
	SourceType           = runtimeimpl.SourceType
	PolicyEffect         = runtimeimpl.PolicyEffect
	ObligationKind       = runtimeimpl.ObligationKind
	UserMessageStatus    = runtimeimpl.UserMessageStatus
	UserMessageType      = runtimeimpl.UserMessageType
	EventType            = runtimeimpl.EventType
	ReplayMode           = runtimeimpl.ReplayMode
	RunTimelineKind      = runtimeimpl.RunTimelineKind
	TraceSpanStatus      = runtimeimpl.TraceSpanStatus
	PolicyOperation      = runtimeimpl.PolicyOperation
	AwaitMode            = runtimeimpl.AwaitMode
	OnDependencyFailed   = runtimeimpl.OnDependencyFailed
	AddressKind          = runtimeimpl.AddressKind
	Address              = runtimeimpl.Address
	AgentProfile         = runtimeimpl.AgentProfile
	BlackboardFilter     = runtimeimpl.BlackboardFilter
)

const (
	RunStatusCreated           = runtimeimpl.RunStatusCreated
	RunStatusPlanning          = runtimeimpl.RunStatusPlanning
	RunStatusValidating        = runtimeimpl.RunStatusValidating
	RunStatusRouting           = runtimeimpl.RunStatusRouting
	RunStatusDispatching       = runtimeimpl.RunStatusDispatching
	RunStatusRunning           = runtimeimpl.RunStatusRunning
	RunStatusWaitingUserInput  = runtimeimpl.RunStatusWaitingUserInput
	RunStatusWaitingApproval   = runtimeimpl.RunStatusWaitingApproval
	RunStatusExecuting         = runtimeimpl.RunStatusExecuting
	RunStatusReconcileRequired = runtimeimpl.RunStatusReconcileRequired
	RunStatusComposingResponse = runtimeimpl.RunStatusComposingResponse
	RunStatusCompleted         = runtimeimpl.RunStatusCompleted
	RunStatusFailed            = runtimeimpl.RunStatusFailed
	RunStatusBlocked           = runtimeimpl.RunStatusBlocked
	RunStatusCancelled         = runtimeimpl.RunStatusCancelled

	TaskTypeWorker   = runtimeimpl.TaskTypeWorker
	TaskTypeResponse = runtimeimpl.TaskTypeResponse

	AwaitModeAll    = runtimeimpl.AwaitModeAll
	AwaitModeAny    = runtimeimpl.AwaitModeAny
	AwaitModeQuorum = runtimeimpl.AwaitModeQuorum

	OnDependencyFailedContinue = runtimeimpl.OnDependencyFailedContinue
	OnDependencyFailedSkip     = runtimeimpl.OnDependencyFailedSkip
	OnDependencyFailedFail     = runtimeimpl.OnDependencyFailedFail

	AddressKindAgent = runtimeimpl.AddressKindAgent
	AddressKindRole  = runtimeimpl.AddressKindRole
	AddressKindGroup = runtimeimpl.AddressKindGroup

	TaskStatusCreated           = runtimeimpl.TaskStatusCreated
	TaskStatusPlanned           = runtimeimpl.TaskStatusPlanned
	TaskStatusValidated         = runtimeimpl.TaskStatusValidated
	TaskStatusRouted            = runtimeimpl.TaskStatusRouted
	TaskStatusWaitingDependency = runtimeimpl.TaskStatusWaitingDependency
	TaskStatusDispatched        = runtimeimpl.TaskStatusDispatched
	TaskStatusRunning           = runtimeimpl.TaskStatusRunning
	TaskStatusPaused            = runtimeimpl.TaskStatusPaused
	TaskStatusWaitingUserInput  = runtimeimpl.TaskStatusWaitingUserInput
	TaskStatusReconcileRequired = runtimeimpl.TaskStatusReconcileRequired
	TaskStatusBlocked           = runtimeimpl.TaskStatusBlocked
	TaskStatusCompleted         = runtimeimpl.TaskStatusCompleted
	TaskStatusFailed            = runtimeimpl.TaskStatusFailed
	TaskStatusCancelled         = runtimeimpl.TaskStatusCancelled

	HolderAgent     = runtimeimpl.HolderAgent
	HolderComponent = runtimeimpl.HolderComponent

	LeaseStatusActive   = runtimeimpl.LeaseStatusActive
	LeaseStatusReleased = runtimeimpl.LeaseStatusReleased
	LeaseStatusExpired  = runtimeimpl.LeaseStatusExpired

	ReportStatusSuccess            = runtimeimpl.ReportStatusSuccess
	ReportStatusPartialSuccess     = runtimeimpl.ReportStatusPartialSuccess
	ReportStatusFailed             = runtimeimpl.ReportStatusFailed
	ReportStatusBlocked            = runtimeimpl.ReportStatusBlocked
	ReportStatusNeedsHandoff       = runtimeimpl.ReportStatusNeedsHandoff
	ReportStatusNeedsApproval      = runtimeimpl.ReportStatusNeedsApproval
	ReportStatusNeedsClarification = runtimeimpl.ReportStatusNeedsClarification

	ActionAttemptCreated   = runtimeimpl.ActionAttemptCreated
	ActionAttemptRunning   = runtimeimpl.ActionAttemptRunning
	ActionAttemptSucceeded = runtimeimpl.ActionAttemptSucceeded
	ActionAttemptFailed    = runtimeimpl.ActionAttemptFailed
	ActionAttemptTimeout   = runtimeimpl.ActionAttemptTimeout
	ActionAttemptUnknown   = runtimeimpl.ActionAttemptUnknown
	ActionAttemptCancelled = runtimeimpl.ActionAttemptCancelled

	ToolEffectReadOnly           = runtimeimpl.ToolEffectReadOnly
	ToolEffectWrite              = runtimeimpl.ToolEffectWrite
	ToolEffectExternalSideEffect = runtimeimpl.ToolEffectExternalSideEffect

	BlackboardVisibilityInternal             = runtimeimpl.BlackboardVisibilityInternal
	BlackboardVisibilityAgentVisible         = runtimeimpl.BlackboardVisibilityAgentVisible
	BlackboardVisibilityUserVisibleCandidate = runtimeimpl.BlackboardVisibilityUserVisibleCandidate
	BlackboardVisibilityUserVisible          = runtimeimpl.BlackboardVisibilityUserVisible

	BlackboardItemClaim          = runtimeimpl.BlackboardItemClaim
	BlackboardItemEvidence       = runtimeimpl.BlackboardItemEvidence
	BlackboardItemFinding        = runtimeimpl.BlackboardItemFinding
	BlackboardItemArtifactRef    = runtimeimpl.BlackboardItemArtifactRef
	BlackboardItemContext        = runtimeimpl.BlackboardItemContext
	BlackboardItemTaskOutput     = runtimeimpl.BlackboardItemTaskOutput
	BlackboardItemHandoffContext = runtimeimpl.BlackboardItemHandoffContext

	SourceAgent     = runtimeimpl.SourceAgent
	SourceComponent = runtimeimpl.SourceComponent
	SourceTool      = runtimeimpl.SourceTool
	SourceSystem    = runtimeimpl.SourceSystem

	PolicyEffectAllow           = runtimeimpl.PolicyEffectAllow
	PolicyEffectDeny            = runtimeimpl.PolicyEffectDeny
	PolicyEffectRequireApproval = runtimeimpl.PolicyEffectRequireApproval
	PolicyEffectPause           = runtimeimpl.PolicyEffectPause
	PolicyEffectAbort           = runtimeimpl.PolicyEffectAbort

	ObligationRedactFields           = runtimeimpl.ObligationRedactFields
	ObligationSelectorOnly           = runtimeimpl.ObligationSelectorOnly
	ObligationRequireHumanApproval   = runtimeimpl.ObligationRequireHumanApproval
	ObligationHideInternalTrace      = runtimeimpl.ObligationHideInternalTrace
	ObligationMaskToolOutput         = runtimeimpl.ObligationMaskToolOutput
	ObligationRestrictHandoffContext = runtimeimpl.ObligationRestrictHandoffContext

	UserMessageComposed  = runtimeimpl.UserMessageComposed
	UserMessageQueued    = runtimeimpl.UserMessageQueued
	UserMessagePublished = runtimeimpl.UserMessagePublished
	UserMessageFailed    = runtimeimpl.UserMessageFailed
	UserMessageCancelled = runtimeimpl.UserMessageCancelled

	UserMessageTypeFinalAnswer          = runtimeimpl.UserMessageTypeFinalAnswer
	UserMessageTypeProgressUpdate       = runtimeimpl.UserMessageTypeProgressUpdate
	UserMessageTypeApprovalRequest      = runtimeimpl.UserMessageTypeApprovalRequest
	UserMessageTypeClarificationRequest = runtimeimpl.UserMessageTypeClarificationRequest
	UserMessageTypeExecutionResult      = runtimeimpl.UserMessageTypeExecutionResult
	UserMessageTypeErrorNotice          = runtimeimpl.UserMessageTypeErrorNotice
	UserMessageTypeBlockedNotice        = runtimeimpl.UserMessageTypeBlockedNotice

	EventRunStarted                  = runtimeimpl.EventRunStarted
	EventRunStatusChanged            = runtimeimpl.EventRunStatusChanged
	EventIntentAnalyzed              = runtimeimpl.EventIntentAnalyzed
	EventPlanCreated                 = runtimeimpl.EventPlanCreated
	EventPlanValidated               = runtimeimpl.EventPlanValidated
	EventRoutingPlanCreated          = runtimeimpl.EventRoutingPlanCreated
	EventTaskCreated                 = runtimeimpl.EventTaskCreated
	EventTaskDispatched              = runtimeimpl.EventTaskDispatched
	EventEnvelopeAcked               = runtimeimpl.EventEnvelopeAcked
	EventEnvelopeDeadLettered        = runtimeimpl.EventEnvelopeDeadLettered
	EventTaskExecutionAcquired       = runtimeimpl.EventTaskExecutionAcquired
	EventTaskExecutionHeartbeat      = runtimeimpl.EventTaskExecutionHeartbeat
	EventTaskExecutionReleased       = runtimeimpl.EventTaskExecutionReleased
	EventTypedReportSubmitted        = runtimeimpl.EventTypedReportSubmitted
	EventTaskCompleted               = runtimeimpl.EventTaskCompleted
	EventTaskFailed                  = runtimeimpl.EventTaskFailed
	EventTaskBlocked                 = runtimeimpl.EventTaskBlocked
	EventTaskPaused                  = runtimeimpl.EventTaskPaused
	EventUserInputSubmitted          = runtimeimpl.EventUserInputSubmitted
	EventResumeTokenCreated          = runtimeimpl.EventResumeTokenCreated
	EventActionReconcileRequired     = runtimeimpl.EventActionReconcileRequired
	EventActionAttemptStarted        = runtimeimpl.EventActionAttemptStarted
	EventActionAttemptUpdated        = runtimeimpl.EventActionAttemptUpdated
	EventBlackboardItemWritten       = runtimeimpl.EventBlackboardItemWritten
	EventHandoffRequested            = runtimeimpl.EventHandoffRequested
	EventHandoffApplied              = runtimeimpl.EventHandoffApplied
	EventHandoffEnvelopeQueued       = runtimeimpl.EventHandoffEnvelopeQueued
	EventApprovalRequested           = runtimeimpl.EventApprovalRequested
	EventApprovalDecided             = runtimeimpl.EventApprovalDecided
	EventTaskOwnerChanged            = runtimeimpl.EventTaskOwnerChanged
	EventPolicyObligationFailed      = runtimeimpl.EventPolicyObligationFailed
	EventResponseTaskCreated         = runtimeimpl.EventResponseTaskCreated
	EventSystemResponseBypassAudited = runtimeimpl.EventSystemResponseBypassAudited
	EventUserMessageComposed         = runtimeimpl.EventUserMessageComposed
	EventUserMessagePolicyChecked    = runtimeimpl.EventUserMessagePolicyChecked
	EventUserMessageQueued           = runtimeimpl.EventUserMessageQueued
	EventResponsePublished           = runtimeimpl.EventResponsePublished
	EventResponsePublishFailed       = runtimeimpl.EventResponsePublishFailed
	EventTaskMonitorDecision         = runtimeimpl.EventTaskMonitorDecision
	EventMailboxRetryScheduled       = runtimeimpl.EventMailboxRetryScheduled
	EventTraceSpanStarted            = runtimeimpl.EventTraceSpanStarted
	EventTraceSpanEnded              = runtimeimpl.EventTraceSpanEnded

	ReplayModeAudit    = runtimeimpl.ReplayModeAudit
	ReplayModeRecovery = runtimeimpl.ReplayModeRecovery

	RunTimelineKindControl  = runtimeimpl.RunTimelineKindControl
	RunTimelineKindWork     = runtimeimpl.RunTimelineKindWork
	RunTimelineKindResponse = runtimeimpl.RunTimelineKindResponse

	TraceSpanStarted = runtimeimpl.TraceSpanStarted
	TraceSpanEnded   = runtimeimpl.TraceSpanEnded
	TraceSpanFailed  = runtimeimpl.TraceSpanFailed

	PolicyOperationDispatch        = runtimeimpl.PolicyOperationDispatch
	PolicyOperationBlackboardRead  = runtimeimpl.PolicyOperationBlackboardRead
	PolicyOperationBlackboardWrite = runtimeimpl.PolicyOperationBlackboardWrite
	PolicyOperationHandoff         = runtimeimpl.PolicyOperationHandoff
	PolicyOperationToolCall        = runtimeimpl.PolicyOperationToolCall
	PolicyOperationAction          = runtimeimpl.PolicyOperationAction
	PolicyOperationResponseCompose = runtimeimpl.PolicyOperationResponseCompose
	PolicyOperationResponsePublish = runtimeimpl.PolicyOperationResponsePublish
)

type (
	Run                = runtimeimpl.Run
	Task               = runtimeimpl.Task
	TaskExecutionLease = runtimeimpl.TaskExecutionLease
	TypedReport        = runtimeimpl.TypedReport
	ActionOutcome      = runtimeimpl.ActionOutcome
	Tool               = runtimeimpl.Tool
	RetryPolicy        = runtimeimpl.RetryPolicy
	SourceIdentity     = runtimeimpl.SourceIdentity
	BlackboardItem     = runtimeimpl.BlackboardItem
	BlackboardSelector = runtimeimpl.BlackboardSelector
	TaskEnvelope       = runtimeimpl.TaskEnvelope
	PolicyDecision     = runtimeimpl.PolicyDecision
	PolicyObligation   = runtimeimpl.PolicyObligation
	UserMessage        = runtimeimpl.UserMessage
	ResumeToken        = runtimeimpl.ResumeToken
	Flow               = runtimeimpl.Flow
	Event              = runtimeimpl.Event
	Projection         = runtimeimpl.Projection
	ReplaySideEffects  = runtimeimpl.ReplaySideEffects
	TraceSpan          = runtimeimpl.TraceSpan
	RunTimelineItem    = runtimeimpl.RunTimelineItem

	Intent           = runtimeimpl.Intent
	TodoPlan         = runtimeimpl.TodoPlan
	RoutingPlan      = runtimeimpl.RoutingPlan
	TaskRoute        = runtimeimpl.TaskRoute
	HandoffRequest   = runtimeimpl.HandoffRequest
	ActionAttempt    = runtimeimpl.ActionAttempt
	ApprovalRequest  = runtimeimpl.ApprovalRequest
	ApprovalDecision = runtimeimpl.ApprovalDecision

	StartRunCommand               = runtimeimpl.StartRunCommand
	CreateTaskCommand             = runtimeimpl.CreateTaskCommand
	TransitionRunCommand          = runtimeimpl.TransitionRunCommand
	TransitionTaskCommand         = runtimeimpl.TransitionTaskCommand
	AdvanceRunCommand             = runtimeimpl.AdvanceRunCommand
	DispatchTaskCommand           = runtimeimpl.DispatchTaskCommand
	FanOutDispatchTaskCommand     = runtimeimpl.FanOutDispatchTaskCommand
	AcquireTaskExecutionCommand   = runtimeimpl.AcquireTaskExecutionCommand
	HeartbeatTaskExecutionCommand = runtimeimpl.HeartbeatTaskExecutionCommand
	ReleaseTaskExecutionCommand   = runtimeimpl.ReleaseTaskExecutionCommand
	AckEnvelopeCommand            = runtimeimpl.AckEnvelopeCommand
	DeadLetterCommand             = runtimeimpl.DeadLetterCommand
	SubmitTypedReportCommand      = runtimeimpl.SubmitTypedReportCommand
	SubmitUserInputCommand        = runtimeimpl.SubmitUserInputCommand
	HandoffCommand                = runtimeimpl.HandoffCommand
	SubmitResponseOutputCommand   = runtimeimpl.SubmitResponseOutputCommand
	PublishResponseCommand        = runtimeimpl.PublishResponseCommand
	RequestApprovalCommand        = runtimeimpl.RequestApprovalCommand
	DecideApprovalCommand         = runtimeimpl.DecideApprovalCommand
	RecoverResumeTokenCommand     = runtimeimpl.RecoverResumeTokenCommand
	StartActionAttemptCommand     = runtimeimpl.StartActionAttemptCommand
	CompleteActionAttemptCommand  = runtimeimpl.CompleteActionAttemptCommand
	StartTraceSpanCommand         = runtimeimpl.StartTraceSpanCommand
	EndTraceSpanCommand           = runtimeimpl.EndTraceSpanCommand

	ToolInvocation       = runtimeimpl.ToolInvocation
	ToolInvocationResult = runtimeimpl.ToolInvocationResult

	RuntimeCommand        = runtimeimpl.RuntimeCommand
	PolicyEngine          = runtimeimpl.PolicyEngine
	OutputGateway         = runtimeimpl.OutputGateway
	UserTimelineProjector = runtimeimpl.UserTimelineProjector
	Projector             = runtimeimpl.Projector
	IntentAnalyzer        = runtimeimpl.IntentAnalyzer
	Planner               = runtimeimpl.Planner
	PlanValidator         = runtimeimpl.PlanValidator
	TaskRouter            = runtimeimpl.TaskRouter
	Dispatcher            = runtimeimpl.Dispatcher
	TaskMonitor           = runtimeimpl.TaskMonitor
	PolicyRequest         = runtimeimpl.PolicyRequest
	TaskMonitorDecision   = runtimeimpl.TaskMonitorDecision
	PipelineComponents    = runtimeimpl.PipelineComponents
	MessagePolicyChecker  = runtimeimpl.MessagePolicyChecker
)
