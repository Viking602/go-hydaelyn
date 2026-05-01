package orchestrator

import core "github.com/Viking602/go-hydaelyn/internal/core"

var (
	ErrNotFound                = core.ErrNotFound
	ErrTerminalState           = core.ErrTerminalState
	ErrStaleTaskVersion        = core.ErrStaleTaskVersion
	ErrLeaseHolderMismatch     = core.ErrLeaseHolderMismatch
	ErrLeaseNotActive          = core.ErrLeaseNotActive
	ErrOwnerMismatch           = core.ErrOwnerMismatch
	ErrActionTaskRequired      = core.ErrActionTaskRequired
	ErrActionReconcileRequired = core.ErrActionReconcileRequired
	ErrResponseTaskRequired    = core.ErrResponseTaskRequired
	ErrPolicyDenied            = core.ErrPolicyDenied
	ErrPolicyObligationFailed  = core.ErrPolicyObligationFailed
	ErrFlowBypass              = core.ErrFlowBypass
	ErrHandoffCycle            = core.ErrHandoffCycle
	ErrHandoffDepthExceeded    = core.ErrHandoffDepthExceeded
	ErrInvalidCommand          = core.ErrInvalidCommand
	ErrInvalidTransition       = core.ErrInvalidTransition
	ErrCompletionCriteriaUnmet = core.ErrCompletionCriteriaUnmet
	ErrDependencyUnmet         = core.ErrDependencyUnmet
	ErrDependencyFailed        = core.ErrDependencyFailed
	ErrInvalidAddress          = core.ErrInvalidAddress
	ErrNoRecipients            = core.ErrNoRecipients
	ErrSubscriptionClosed      = core.ErrSubscriptionClosed
	ErrWaitTimeout             = core.ErrWaitTimeout
)

type (
	RunStatus            = core.RunStatus
	TaskType             = core.TaskType
	TaskStatus           = core.TaskStatus
	HolderType           = core.HolderType
	LeaseStatus          = core.LeaseStatus
	ReportStatus         = core.ReportStatus
	ActionAttemptStatus  = core.ActionAttemptStatus
	ToolEffectType       = core.ToolEffectType
	BlackboardVisibility = core.BlackboardVisibility
	BlackboardItemType   = core.BlackboardItemType
	SourceType           = core.SourceType
	PolicyEffect         = core.PolicyEffect
	ObligationKind       = core.ObligationKind
	UserMessageStatus    = core.UserMessageStatus
	UserMessageType      = core.UserMessageType
	EventType            = core.EventType
	ReplayMode           = core.ReplayMode
	RunTimelineKind      = core.RunTimelineKind
	TraceSpanStatus      = core.TraceSpanStatus
	PolicyOperation      = core.PolicyOperation
	AwaitMode            = core.AwaitMode
	OnDependencyFailed   = core.OnDependencyFailed
	AddressKind          = core.AddressKind
	Address              = core.Address
	AgentProfile         = core.AgentProfile
	BlackboardFilter     = core.BlackboardFilter
)

const (
	RunStatusCreated           = core.RunStatusCreated
	RunStatusPlanning          = core.RunStatusPlanning
	RunStatusValidating        = core.RunStatusValidating
	RunStatusRouting           = core.RunStatusRouting
	RunStatusDispatching       = core.RunStatusDispatching
	RunStatusRunning           = core.RunStatusRunning
	RunStatusWaitingUserInput  = core.RunStatusWaitingUserInput
	RunStatusWaitingApproval   = core.RunStatusWaitingApproval
	RunStatusExecuting         = core.RunStatusExecuting
	RunStatusReconcileRequired = core.RunStatusReconcileRequired
	RunStatusComposingResponse = core.RunStatusComposingResponse
	RunStatusCompleted         = core.RunStatusCompleted
	RunStatusFailed            = core.RunStatusFailed
	RunStatusBlocked           = core.RunStatusBlocked
	RunStatusCancelled         = core.RunStatusCancelled

	TaskTypeWorker   = core.TaskTypeWorker
	TaskTypeResponse = core.TaskTypeResponse

	AwaitModeAll    = core.AwaitModeAll
	AwaitModeAny    = core.AwaitModeAny
	AwaitModeQuorum = core.AwaitModeQuorum

	OnDependencyFailedContinue = core.OnDependencyFailedContinue
	OnDependencyFailedSkip     = core.OnDependencyFailedSkip
	OnDependencyFailedFail     = core.OnDependencyFailedFail

	AddressKindAgent = core.AddressKindAgent
	AddressKindRole  = core.AddressKindRole
	AddressKindGroup = core.AddressKindGroup

	TaskStatusCreated           = core.TaskStatusCreated
	TaskStatusPlanned           = core.TaskStatusPlanned
	TaskStatusValidated         = core.TaskStatusValidated
	TaskStatusRouted            = core.TaskStatusRouted
	TaskStatusWaitingDependency = core.TaskStatusWaitingDependency
	TaskStatusDispatched        = core.TaskStatusDispatched
	TaskStatusRunning           = core.TaskStatusRunning
	TaskStatusPaused            = core.TaskStatusPaused
	TaskStatusWaitingUserInput  = core.TaskStatusWaitingUserInput
	TaskStatusReconcileRequired = core.TaskStatusReconcileRequired
	TaskStatusBlocked           = core.TaskStatusBlocked
	TaskStatusCompleted         = core.TaskStatusCompleted
	TaskStatusFailed            = core.TaskStatusFailed
	TaskStatusCancelled         = core.TaskStatusCancelled

	HolderAgent     = core.HolderAgent
	HolderComponent = core.HolderComponent

	LeaseStatusActive   = core.LeaseStatusActive
	LeaseStatusReleased = core.LeaseStatusReleased
	LeaseStatusExpired  = core.LeaseStatusExpired

	ReportStatusSuccess            = core.ReportStatusSuccess
	ReportStatusPartialSuccess     = core.ReportStatusPartialSuccess
	ReportStatusFailed             = core.ReportStatusFailed
	ReportStatusBlocked            = core.ReportStatusBlocked
	ReportStatusNeedsHandoff       = core.ReportStatusNeedsHandoff
	ReportStatusNeedsApproval      = core.ReportStatusNeedsApproval
	ReportStatusNeedsClarification = core.ReportStatusNeedsClarification

	ActionAttemptCreated   = core.ActionAttemptCreated
	ActionAttemptRunning   = core.ActionAttemptRunning
	ActionAttemptSucceeded = core.ActionAttemptSucceeded
	ActionAttemptFailed    = core.ActionAttemptFailed
	ActionAttemptTimeout   = core.ActionAttemptTimeout
	ActionAttemptUnknown   = core.ActionAttemptUnknown
	ActionAttemptCancelled = core.ActionAttemptCancelled

	ToolEffectReadOnly           = core.ToolEffectReadOnly
	ToolEffectWrite              = core.ToolEffectWrite
	ToolEffectExternalSideEffect = core.ToolEffectExternalSideEffect

	BlackboardVisibilityInternal             = core.BlackboardVisibilityInternal
	BlackboardVisibilityAgentVisible         = core.BlackboardVisibilityAgentVisible
	BlackboardVisibilityUserVisibleCandidate = core.BlackboardVisibilityUserVisibleCandidate
	BlackboardVisibilityUserVisible          = core.BlackboardVisibilityUserVisible

	BlackboardItemClaim          = core.BlackboardItemClaim
	BlackboardItemEvidence       = core.BlackboardItemEvidence
	BlackboardItemFinding        = core.BlackboardItemFinding
	BlackboardItemArtifactRef    = core.BlackboardItemArtifactRef
	BlackboardItemContext        = core.BlackboardItemContext
	BlackboardItemTaskOutput     = core.BlackboardItemTaskOutput
	BlackboardItemHandoffContext = core.BlackboardItemHandoffContext

	SourceAgent     = core.SourceAgent
	SourceComponent = core.SourceComponent
	SourceTool      = core.SourceTool
	SourceSystem    = core.SourceSystem

	PolicyEffectAllow           = core.PolicyEffectAllow
	PolicyEffectDeny            = core.PolicyEffectDeny
	PolicyEffectRequireApproval = core.PolicyEffectRequireApproval
	PolicyEffectPause           = core.PolicyEffectPause
	PolicyEffectAbort           = core.PolicyEffectAbort

	ObligationRedactFields           = core.ObligationRedactFields
	ObligationSelectorOnly           = core.ObligationSelectorOnly
	ObligationRequireHumanApproval   = core.ObligationRequireHumanApproval
	ObligationHideInternalTrace      = core.ObligationHideInternalTrace
	ObligationMaskToolOutput         = core.ObligationMaskToolOutput
	ObligationRestrictHandoffContext = core.ObligationRestrictHandoffContext

	UserMessageComposed  = core.UserMessageComposed
	UserMessageQueued    = core.UserMessageQueued
	UserMessagePublished = core.UserMessagePublished
	UserMessageFailed    = core.UserMessageFailed
	UserMessageCancelled = core.UserMessageCancelled

	UserMessageTypeFinalAnswer          = core.UserMessageTypeFinalAnswer
	UserMessageTypeProgressUpdate       = core.UserMessageTypeProgressUpdate
	UserMessageTypeApprovalRequest      = core.UserMessageTypeApprovalRequest
	UserMessageTypeClarificationRequest = core.UserMessageTypeClarificationRequest
	UserMessageTypeExecutionResult      = core.UserMessageTypeExecutionResult
	UserMessageTypeErrorNotice          = core.UserMessageTypeErrorNotice
	UserMessageTypeBlockedNotice        = core.UserMessageTypeBlockedNotice

	EventRunStarted                  = core.EventRunStarted
	EventRunStatusChanged            = core.EventRunStatusChanged
	EventIntentAnalyzed              = core.EventIntentAnalyzed
	EventPlanCreated                 = core.EventPlanCreated
	EventPlanValidated               = core.EventPlanValidated
	EventRoutingPlanCreated          = core.EventRoutingPlanCreated
	EventTaskCreated                 = core.EventTaskCreated
	EventTaskDispatched              = core.EventTaskDispatched
	EventEnvelopeAcked               = core.EventEnvelopeAcked
	EventEnvelopeDeadLettered        = core.EventEnvelopeDeadLettered
	EventTaskExecutionAcquired       = core.EventTaskExecutionAcquired
	EventTaskExecutionHeartbeat      = core.EventTaskExecutionHeartbeat
	EventTaskExecutionReleased       = core.EventTaskExecutionReleased
	EventTypedReportSubmitted        = core.EventTypedReportSubmitted
	EventTaskCompleted               = core.EventTaskCompleted
	EventTaskFailed                  = core.EventTaskFailed
	EventTaskBlocked                 = core.EventTaskBlocked
	EventTaskPaused                  = core.EventTaskPaused
	EventUserInputSubmitted          = core.EventUserInputSubmitted
	EventResumeTokenCreated          = core.EventResumeTokenCreated
	EventActionReconcileRequired     = core.EventActionReconcileRequired
	EventActionAttemptStarted        = core.EventActionAttemptStarted
	EventActionAttemptUpdated        = core.EventActionAttemptUpdated
	EventBlackboardItemWritten       = core.EventBlackboardItemWritten
	EventHandoffRequested            = core.EventHandoffRequested
	EventHandoffApplied              = core.EventHandoffApplied
	EventHandoffEnvelopeQueued       = core.EventHandoffEnvelopeQueued
	EventApprovalRequested           = core.EventApprovalRequested
	EventApprovalDecided             = core.EventApprovalDecided
	EventTaskOwnerChanged            = core.EventTaskOwnerChanged
	EventPolicyObligationFailed      = core.EventPolicyObligationFailed
	EventResponseTaskCreated         = core.EventResponseTaskCreated
	EventSystemResponseBypassAudited = core.EventSystemResponseBypassAudited
	EventUserMessageComposed         = core.EventUserMessageComposed
	EventUserMessagePolicyChecked    = core.EventUserMessagePolicyChecked
	EventUserMessageQueued           = core.EventUserMessageQueued
	EventResponsePublished           = core.EventResponsePublished
	EventResponsePublishFailed       = core.EventResponsePublishFailed
	EventTaskMonitorDecision         = core.EventTaskMonitorDecision
	EventMailboxRetryScheduled       = core.EventMailboxRetryScheduled
	EventTraceSpanStarted            = core.EventTraceSpanStarted
	EventTraceSpanEnded              = core.EventTraceSpanEnded

	ReplayModeAudit    = core.ReplayModeAudit
	ReplayModeRecovery = core.ReplayModeRecovery

	RunTimelineKindControl  = core.RunTimelineKindControl
	RunTimelineKindWork     = core.RunTimelineKindWork
	RunTimelineKindResponse = core.RunTimelineKindResponse

	TraceSpanStarted = core.TraceSpanStarted
	TraceSpanEnded   = core.TraceSpanEnded
	TraceSpanFailed  = core.TraceSpanFailed

	PolicyOperationDispatch        = core.PolicyOperationDispatch
	PolicyOperationBlackboardRead  = core.PolicyOperationBlackboardRead
	PolicyOperationBlackboardWrite = core.PolicyOperationBlackboardWrite
	PolicyOperationHandoff         = core.PolicyOperationHandoff
	PolicyOperationToolCall        = core.PolicyOperationToolCall
	PolicyOperationAction          = core.PolicyOperationAction
	PolicyOperationResponseCompose = core.PolicyOperationResponseCompose
	PolicyOperationResponsePublish = core.PolicyOperationResponsePublish
)

type (
	Run                = core.Run
	Task               = core.Task
	TaskExecutionLease = core.TaskExecutionLease
	TypedReport        = core.TypedReport
	ActionOutcome      = core.ActionOutcome
	Tool               = core.Tool
	RetryPolicy        = core.RetryPolicy
	SourceIdentity     = core.SourceIdentity
	BlackboardItem     = core.BlackboardItem
	BlackboardSelector = core.BlackboardSelector
	TaskEnvelope       = core.TaskEnvelope
	PolicyDecision     = core.PolicyDecision
	PolicyObligation   = core.PolicyObligation
	UserMessage        = core.UserMessage
	ResumeToken        = core.ResumeToken
	Flow               = core.Flow
	Event              = core.Event
	Projection         = core.Projection
	ReplaySideEffects  = core.ReplaySideEffects
	TraceSpan          = core.TraceSpan
	RunTimelineItem    = core.RunTimelineItem

	Intent           = core.Intent
	TodoPlan         = core.TodoPlan
	RoutingPlan      = core.RoutingPlan
	TaskRoute        = core.TaskRoute
	HandoffRequest   = core.HandoffRequest
	ActionAttempt    = core.ActionAttempt
	ApprovalRequest  = core.ApprovalRequest
	ApprovalDecision = core.ApprovalDecision

	StartRunCommand               = core.StartRunCommand
	CreateTaskCommand             = core.CreateTaskCommand
	TransitionRunCommand          = core.TransitionRunCommand
	TransitionTaskCommand         = core.TransitionTaskCommand
	AdvanceRunCommand             = core.AdvanceRunCommand
	DispatchTaskCommand           = core.DispatchTaskCommand
	FanOutDispatchTaskCommand     = core.FanOutDispatchTaskCommand
	WriteBlackboardItemCommand    = core.WriteBlackboardItemCommand
	AcquireTaskExecutionCommand   = core.AcquireTaskExecutionCommand
	HeartbeatTaskExecutionCommand = core.HeartbeatTaskExecutionCommand
	ReleaseTaskExecutionCommand   = core.ReleaseTaskExecutionCommand
	AckEnvelopeCommand            = core.AckEnvelopeCommand
	DeadLetterCommand             = core.DeadLetterCommand
	SubmitTypedReportCommand      = core.SubmitTypedReportCommand
	SubmitUserInputCommand        = core.SubmitUserInputCommand
	HandoffCommand                = core.HandoffCommand
	SubmitResponseOutputCommand   = core.SubmitResponseOutputCommand
	PublishResponseCommand        = core.PublishResponseCommand
	RequestApprovalCommand        = core.RequestApprovalCommand
	DecideApprovalCommand         = core.DecideApprovalCommand
	RecoverResumeTokenCommand     = core.RecoverResumeTokenCommand
	StartActionAttemptCommand     = core.StartActionAttemptCommand
	CompleteActionAttemptCommand  = core.CompleteActionAttemptCommand
	StartTraceSpanCommand         = core.StartTraceSpanCommand
	EndTraceSpanCommand           = core.EndTraceSpanCommand

	ToolInvocation       = core.ToolInvocation
	ToolInvocationResult = core.ToolInvocationResult

	Command                  = core.RuntimeCommand
	RuntimeCommand           = core.RuntimeCommand
	StoreProvider            = core.StoreProvider
	UnitOfWork               = core.UnitOfWork
	RunStore                 = core.RunStore
	TaskStore                = core.TaskStore
	EventStore               = core.EventStore
	BlackboardStore          = core.BlackboardStore
	MailboxOutboxStore       = core.MailboxOutboxStore
	UserMessageStore         = core.UserMessageStore
	UserMessageOutboxScanner = core.UserMessageOutboxScanner
	TraceStore               = core.TraceStore
	PolicyEngine             = core.PolicyEngine
	OutputGateway            = core.OutputGateway
	UserTimelineProjector    = core.UserTimelineProjector
	Projector                = core.Projector
	IntentAnalyzer           = core.IntentAnalyzer
	Planner                  = core.Planner
	PlanValidator            = core.PlanValidator
	TaskRouter               = core.TaskRouter
	Dispatcher               = core.Dispatcher
	TaskMonitor              = core.TaskMonitor
	PolicyRequest            = core.PolicyRequest
	TaskMonitorDecision      = core.TaskMonitorDecision
	PipelineComponents       = core.PipelineComponents
	MessagePolicyChecker     = core.MessagePolicyChecker
)
