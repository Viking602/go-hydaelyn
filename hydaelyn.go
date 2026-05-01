// Package hydaelyn is the public façade for the Hydaelyn runner.
//
// All exported names re-route to the orchestrator package; legacy host/team
// runtimes were removed in v2.0.
package hydaelyn

import "github.com/Viking602/go-hydaelyn/orchestrator"

// New constructs the primary Run/Task runner. With no arguments it uses the
// default in-memory configuration; pass Config only when overriding defaults.
func New(configs ...Config) *Runner { return orchestrator.New(configs...) }

func DefaultConfig() Config { return orchestrator.DefaultConfig() }

var (
	ErrNotFound                = orchestrator.ErrNotFound
	ErrTerminalState           = orchestrator.ErrTerminalState
	ErrStaleTaskVersion        = orchestrator.ErrStaleTaskVersion
	ErrLeaseHolderMismatch     = orchestrator.ErrLeaseHolderMismatch
	ErrLeaseNotActive          = orchestrator.ErrLeaseNotActive
	ErrOwnerMismatch           = orchestrator.ErrOwnerMismatch
	ErrActionTaskRequired      = orchestrator.ErrActionTaskRequired
	ErrActionReconcileRequired = orchestrator.ErrActionReconcileRequired
	ErrResponseTaskRequired    = orchestrator.ErrResponseTaskRequired
	ErrPolicyDenied            = orchestrator.ErrPolicyDenied
	ErrPolicyObligationFailed  = orchestrator.ErrPolicyObligationFailed
	ErrFlowBypass              = orchestrator.ErrFlowBypass
	ErrHandoffCycle            = orchestrator.ErrHandoffCycle
	ErrHandoffDepthExceeded    = orchestrator.ErrHandoffDepthExceeded
	ErrInvalidCommand          = orchestrator.ErrInvalidCommand
	ErrInvalidTransition       = orchestrator.ErrInvalidTransition
	ErrCompletionCriteriaUnmet = orchestrator.ErrCompletionCriteriaUnmet
	ErrDependencyUnmet         = orchestrator.ErrDependencyUnmet
	ErrDependencyFailed        = orchestrator.ErrDependencyFailed
	ErrInvalidAddress          = orchestrator.ErrInvalidAddress
	ErrNoRecipients            = orchestrator.ErrNoRecipients
	ErrSubscriptionClosed      = orchestrator.ErrSubscriptionClosed
	ErrWaitTimeout             = orchestrator.ErrWaitTimeout
)

// Public façade types. Each is a Go type alias for the equivalent type in the
// orchestrator package, so values constructed via either name are
// interchangeable.
type (
	Runner = orchestrator.Runner
	// Deprecated: use Runner.
	Runtime = orchestrator.Runner
	Config  = orchestrator.Config

	RunStatus            = orchestrator.RunStatus
	TaskType             = orchestrator.TaskType
	TaskStatus           = orchestrator.TaskStatus
	HolderType           = orchestrator.HolderType
	LeaseStatus          = orchestrator.LeaseStatus
	ReportStatus         = orchestrator.ReportStatus
	ActionAttemptStatus  = orchestrator.ActionAttemptStatus
	ToolEffectType       = orchestrator.ToolEffectType
	BlackboardVisibility = orchestrator.BlackboardVisibility
	BlackboardItemType   = orchestrator.BlackboardItemType
	SourceType           = orchestrator.SourceType
	PolicyEffect         = orchestrator.PolicyEffect
	ObligationKind       = orchestrator.ObligationKind
	UserMessageStatus    = orchestrator.UserMessageStatus
	UserMessageType      = orchestrator.UserMessageType
	EventType            = orchestrator.EventType
	ReplayMode           = orchestrator.ReplayMode
	RunTimelineKind      = orchestrator.RunTimelineKind
	TraceSpanStatus      = orchestrator.TraceSpanStatus
	PolicyOperation      = orchestrator.PolicyOperation
	AwaitMode            = orchestrator.AwaitMode
	OnDependencyFailed   = orchestrator.OnDependencyFailed
	AddressKind          = orchestrator.AddressKind
	Address              = orchestrator.Address
	AgentProfile         = orchestrator.AgentProfile
	BlackboardFilter     = orchestrator.BlackboardFilter

	Run                = orchestrator.Run
	Task               = orchestrator.Task
	TaskExecutionLease = orchestrator.TaskExecutionLease
	TypedReport        = orchestrator.TypedReport
	ActionOutcome      = orchestrator.ActionOutcome
	Tool               = orchestrator.Tool
	RetryPolicy        = orchestrator.RetryPolicy
	SourceIdentity     = orchestrator.SourceIdentity
	BlackboardItem     = orchestrator.BlackboardItem
	BlackboardSelector = orchestrator.BlackboardSelector
	TaskEnvelope       = orchestrator.TaskEnvelope
	PolicyDecision     = orchestrator.PolicyDecision
	PolicyObligation   = orchestrator.PolicyObligation
	UserMessage        = orchestrator.UserMessage
	ResumeToken        = orchestrator.ResumeToken
	Flow               = orchestrator.Flow
	Event              = orchestrator.Event
	Projection         = orchestrator.Projection
	ReplaySideEffects  = orchestrator.ReplaySideEffects
	TraceSpan          = orchestrator.TraceSpan
	RunTimelineItem    = orchestrator.RunTimelineItem

	Intent           = orchestrator.Intent
	TodoPlan         = orchestrator.TodoPlan
	RoutingPlan      = orchestrator.RoutingPlan
	TaskRoute        = orchestrator.TaskRoute
	HandoffRequest   = orchestrator.HandoffRequest
	ActionAttempt    = orchestrator.ActionAttempt
	ApprovalRequest  = orchestrator.ApprovalRequest
	ApprovalDecision = orchestrator.ApprovalDecision

	StartRunCommand               = orchestrator.StartRunCommand
	CreateTaskCommand             = orchestrator.CreateTaskCommand
	TransitionRunCommand          = orchestrator.TransitionRunCommand
	TransitionTaskCommand         = orchestrator.TransitionTaskCommand
	AdvanceRunCommand             = orchestrator.AdvanceRunCommand
	DispatchTaskCommand           = orchestrator.DispatchTaskCommand
	FanOutDispatchTaskCommand     = orchestrator.FanOutDispatchTaskCommand
	WriteBlackboardItemCommand    = orchestrator.WriteBlackboardItemCommand
	AcquireTaskExecutionCommand   = orchestrator.AcquireTaskExecutionCommand
	HeartbeatTaskExecutionCommand = orchestrator.HeartbeatTaskExecutionCommand
	ReleaseTaskExecutionCommand   = orchestrator.ReleaseTaskExecutionCommand
	AckEnvelopeCommand            = orchestrator.AckEnvelopeCommand
	DeadLetterCommand             = orchestrator.DeadLetterCommand
	SubmitTypedReportCommand      = orchestrator.SubmitTypedReportCommand
	SubmitUserInputCommand        = orchestrator.SubmitUserInputCommand
	HandoffCommand                = orchestrator.HandoffCommand
	SubmitResponseOutputCommand   = orchestrator.SubmitResponseOutputCommand
	PublishResponseCommand        = orchestrator.PublishResponseCommand
	RequestApprovalCommand        = orchestrator.RequestApprovalCommand
	DecideApprovalCommand         = orchestrator.DecideApprovalCommand
	RecoverResumeTokenCommand     = orchestrator.RecoverResumeTokenCommand
	StartActionAttemptCommand     = orchestrator.StartActionAttemptCommand
	CompleteActionAttemptCommand  = orchestrator.CompleteActionAttemptCommand
	StartTraceSpanCommand         = orchestrator.StartTraceSpanCommand
	EndTraceSpanCommand           = orchestrator.EndTraceSpanCommand

	ToolInvocation       = orchestrator.ToolInvocation
	ToolInvocationResult = orchestrator.ToolInvocationResult

	Command                  = orchestrator.Command
	RuntimeCommand           = orchestrator.RuntimeCommand
	StoreProvider            = orchestrator.StoreProvider
	UnitOfWork               = orchestrator.UnitOfWork
	RunStore                 = orchestrator.RunStore
	TaskStore                = orchestrator.TaskStore
	EventStore               = orchestrator.EventStore
	BlackboardStore          = orchestrator.BlackboardStore
	MailboxOutboxStore       = orchestrator.MailboxOutboxStore
	UserMessageStore         = orchestrator.UserMessageStore
	UserMessageOutboxScanner = orchestrator.UserMessageOutboxScanner
	TraceStore               = orchestrator.TraceStore
	PolicyEngine             = orchestrator.PolicyEngine
	OutputGateway            = orchestrator.OutputGateway
	UserTimelineProjector    = orchestrator.UserTimelineProjector
	Projector                = orchestrator.Projector
	IntentAnalyzer           = orchestrator.IntentAnalyzer
	Planner                  = orchestrator.Planner
	PlanValidator            = orchestrator.PlanValidator
	TaskRouter               = orchestrator.TaskRouter
	Dispatcher               = orchestrator.Dispatcher
	TaskMonitor              = orchestrator.TaskMonitor
	PolicyRequest            = orchestrator.PolicyRequest
	TaskMonitorDecision      = orchestrator.TaskMonitorDecision
	PipelineComponents       = orchestrator.PipelineComponents
	MessagePolicyChecker     = orchestrator.MessagePolicyChecker
)

const (
	RunStatusCreated           = orchestrator.RunStatusCreated
	RunStatusPlanning          = orchestrator.RunStatusPlanning
	RunStatusValidating        = orchestrator.RunStatusValidating
	RunStatusRouting           = orchestrator.RunStatusRouting
	RunStatusDispatching       = orchestrator.RunStatusDispatching
	RunStatusRunning           = orchestrator.RunStatusRunning
	RunStatusWaitingUserInput  = orchestrator.RunStatusWaitingUserInput
	RunStatusWaitingApproval   = orchestrator.RunStatusWaitingApproval
	RunStatusExecuting         = orchestrator.RunStatusExecuting
	RunStatusReconcileRequired = orchestrator.RunStatusReconcileRequired
	RunStatusComposingResponse = orchestrator.RunStatusComposingResponse
	RunStatusCompleted         = orchestrator.RunStatusCompleted
	RunStatusFailed            = orchestrator.RunStatusFailed
	RunStatusBlocked           = orchestrator.RunStatusBlocked
	RunStatusCancelled         = orchestrator.RunStatusCancelled

	TaskTypeWorker   = orchestrator.TaskTypeWorker
	TaskTypeResponse = orchestrator.TaskTypeResponse

	AwaitModeAll    = orchestrator.AwaitModeAll
	AwaitModeAny    = orchestrator.AwaitModeAny
	AwaitModeQuorum = orchestrator.AwaitModeQuorum

	OnDependencyFailedContinue = orchestrator.OnDependencyFailedContinue
	OnDependencyFailedSkip     = orchestrator.OnDependencyFailedSkip
	OnDependencyFailedFail     = orchestrator.OnDependencyFailedFail

	AddressKindAgent = orchestrator.AddressKindAgent
	AddressKindRole  = orchestrator.AddressKindRole
	AddressKindGroup = orchestrator.AddressKindGroup

	TaskStatusCreated           = orchestrator.TaskStatusCreated
	TaskStatusPlanned           = orchestrator.TaskStatusPlanned
	TaskStatusValidated         = orchestrator.TaskStatusValidated
	TaskStatusRouted            = orchestrator.TaskStatusRouted
	TaskStatusWaitingDependency = orchestrator.TaskStatusWaitingDependency
	TaskStatusDispatched        = orchestrator.TaskStatusDispatched
	TaskStatusRunning           = orchestrator.TaskStatusRunning
	TaskStatusPaused            = orchestrator.TaskStatusPaused
	TaskStatusWaitingUserInput  = orchestrator.TaskStatusWaitingUserInput
	TaskStatusReconcileRequired = orchestrator.TaskStatusReconcileRequired
	TaskStatusBlocked           = orchestrator.TaskStatusBlocked
	TaskStatusCompleted         = orchestrator.TaskStatusCompleted
	TaskStatusFailed            = orchestrator.TaskStatusFailed
	TaskStatusCancelled         = orchestrator.TaskStatusCancelled

	HolderAgent     = orchestrator.HolderAgent
	HolderComponent = orchestrator.HolderComponent

	LeaseStatusActive   = orchestrator.LeaseStatusActive
	LeaseStatusReleased = orchestrator.LeaseStatusReleased
	LeaseStatusExpired  = orchestrator.LeaseStatusExpired

	ReportStatusSuccess            = orchestrator.ReportStatusSuccess
	ReportStatusPartialSuccess     = orchestrator.ReportStatusPartialSuccess
	ReportStatusFailed             = orchestrator.ReportStatusFailed
	ReportStatusBlocked            = orchestrator.ReportStatusBlocked
	ReportStatusNeedsHandoff       = orchestrator.ReportStatusNeedsHandoff
	ReportStatusNeedsApproval      = orchestrator.ReportStatusNeedsApproval
	ReportStatusNeedsClarification = orchestrator.ReportStatusNeedsClarification

	ActionAttemptCreated   = orchestrator.ActionAttemptCreated
	ActionAttemptRunning   = orchestrator.ActionAttemptRunning
	ActionAttemptSucceeded = orchestrator.ActionAttemptSucceeded
	ActionAttemptFailed    = orchestrator.ActionAttemptFailed
	ActionAttemptTimeout   = orchestrator.ActionAttemptTimeout
	ActionAttemptUnknown   = orchestrator.ActionAttemptUnknown
	ActionAttemptCancelled = orchestrator.ActionAttemptCancelled

	ToolEffectReadOnly           = orchestrator.ToolEffectReadOnly
	ToolEffectWrite              = orchestrator.ToolEffectWrite
	ToolEffectExternalSideEffect = orchestrator.ToolEffectExternalSideEffect

	BlackboardVisibilityInternal             = orchestrator.BlackboardVisibilityInternal
	BlackboardVisibilityAgentVisible         = orchestrator.BlackboardVisibilityAgentVisible
	BlackboardVisibilityUserVisibleCandidate = orchestrator.BlackboardVisibilityUserVisibleCandidate
	BlackboardVisibilityUserVisible          = orchestrator.BlackboardVisibilityUserVisible

	BlackboardItemClaim          = orchestrator.BlackboardItemClaim
	BlackboardItemEvidence       = orchestrator.BlackboardItemEvidence
	BlackboardItemFinding        = orchestrator.BlackboardItemFinding
	BlackboardItemArtifactRef    = orchestrator.BlackboardItemArtifactRef
	BlackboardItemContext        = orchestrator.BlackboardItemContext
	BlackboardItemTaskOutput     = orchestrator.BlackboardItemTaskOutput
	BlackboardItemHandoffContext = orchestrator.BlackboardItemHandoffContext

	SourceAgent     = orchestrator.SourceAgent
	SourceComponent = orchestrator.SourceComponent
	SourceTool      = orchestrator.SourceTool
	SourceSystem    = orchestrator.SourceSystem

	PolicyEffectAllow           = orchestrator.PolicyEffectAllow
	PolicyEffectDeny            = orchestrator.PolicyEffectDeny
	PolicyEffectRequireApproval = orchestrator.PolicyEffectRequireApproval
	PolicyEffectPause           = orchestrator.PolicyEffectPause
	PolicyEffectAbort           = orchestrator.PolicyEffectAbort

	ObligationRedactFields           = orchestrator.ObligationRedactFields
	ObligationSelectorOnly           = orchestrator.ObligationSelectorOnly
	ObligationRequireHumanApproval   = orchestrator.ObligationRequireHumanApproval
	ObligationHideInternalTrace      = orchestrator.ObligationHideInternalTrace
	ObligationMaskToolOutput         = orchestrator.ObligationMaskToolOutput
	ObligationRestrictHandoffContext = orchestrator.ObligationRestrictHandoffContext

	UserMessageComposed  = orchestrator.UserMessageComposed
	UserMessageQueued    = orchestrator.UserMessageQueued
	UserMessagePublished = orchestrator.UserMessagePublished
	UserMessageFailed    = orchestrator.UserMessageFailed
	UserMessageCancelled = orchestrator.UserMessageCancelled

	UserMessageTypeFinalAnswer          = orchestrator.UserMessageTypeFinalAnswer
	UserMessageTypeProgressUpdate       = orchestrator.UserMessageTypeProgressUpdate
	UserMessageTypeApprovalRequest      = orchestrator.UserMessageTypeApprovalRequest
	UserMessageTypeClarificationRequest = orchestrator.UserMessageTypeClarificationRequest
	UserMessageTypeExecutionResult      = orchestrator.UserMessageTypeExecutionResult
	UserMessageTypeErrorNotice          = orchestrator.UserMessageTypeErrorNotice
	UserMessageTypeBlockedNotice        = orchestrator.UserMessageTypeBlockedNotice

	EventRunStarted                  = orchestrator.EventRunStarted
	EventRunStatusChanged            = orchestrator.EventRunStatusChanged
	EventIntentAnalyzed              = orchestrator.EventIntentAnalyzed
	EventPlanCreated                 = orchestrator.EventPlanCreated
	EventPlanValidated               = orchestrator.EventPlanValidated
	EventRoutingPlanCreated          = orchestrator.EventRoutingPlanCreated
	EventTaskCreated                 = orchestrator.EventTaskCreated
	EventTaskDispatched              = orchestrator.EventTaskDispatched
	EventEnvelopeAcked               = orchestrator.EventEnvelopeAcked
	EventEnvelopeDeadLettered        = orchestrator.EventEnvelopeDeadLettered
	EventTaskExecutionAcquired       = orchestrator.EventTaskExecutionAcquired
	EventTaskExecutionHeartbeat      = orchestrator.EventTaskExecutionHeartbeat
	EventTaskExecutionReleased       = orchestrator.EventTaskExecutionReleased
	EventTypedReportSubmitted        = orchestrator.EventTypedReportSubmitted
	EventTaskCompleted               = orchestrator.EventTaskCompleted
	EventTaskFailed                  = orchestrator.EventTaskFailed
	EventTaskBlocked                 = orchestrator.EventTaskBlocked
	EventTaskPaused                  = orchestrator.EventTaskPaused
	EventUserInputSubmitted          = orchestrator.EventUserInputSubmitted
	EventResumeTokenCreated          = orchestrator.EventResumeTokenCreated
	EventActionReconcileRequired     = orchestrator.EventActionReconcileRequired
	EventActionAttemptStarted        = orchestrator.EventActionAttemptStarted
	EventActionAttemptUpdated        = orchestrator.EventActionAttemptUpdated
	EventBlackboardItemWritten       = orchestrator.EventBlackboardItemWritten
	EventHandoffRequested            = orchestrator.EventHandoffRequested
	EventHandoffApplied              = orchestrator.EventHandoffApplied
	EventHandoffEnvelopeQueued       = orchestrator.EventHandoffEnvelopeQueued
	EventApprovalRequested           = orchestrator.EventApprovalRequested
	EventApprovalDecided             = orchestrator.EventApprovalDecided
	EventTaskOwnerChanged            = orchestrator.EventTaskOwnerChanged
	EventPolicyObligationFailed      = orchestrator.EventPolicyObligationFailed
	EventResponseTaskCreated         = orchestrator.EventResponseTaskCreated
	EventSystemResponseBypassAudited = orchestrator.EventSystemResponseBypassAudited
	EventUserMessageComposed         = orchestrator.EventUserMessageComposed
	EventUserMessagePolicyChecked    = orchestrator.EventUserMessagePolicyChecked
	EventUserMessageQueued           = orchestrator.EventUserMessageQueued
	EventResponsePublished           = orchestrator.EventResponsePublished
	EventResponsePublishFailed       = orchestrator.EventResponsePublishFailed
	EventTaskMonitorDecision         = orchestrator.EventTaskMonitorDecision
	EventMailboxRetryScheduled       = orchestrator.EventMailboxRetryScheduled
	EventTraceSpanStarted            = orchestrator.EventTraceSpanStarted
	EventTraceSpanEnded              = orchestrator.EventTraceSpanEnded

	ReplayModeAudit    = orchestrator.ReplayModeAudit
	ReplayModeRecovery = orchestrator.ReplayModeRecovery

	RunTimelineKindControl  = orchestrator.RunTimelineKindControl
	RunTimelineKindWork     = orchestrator.RunTimelineKindWork
	RunTimelineKindResponse = orchestrator.RunTimelineKindResponse

	TraceSpanStarted = orchestrator.TraceSpanStarted
	TraceSpanEnded   = orchestrator.TraceSpanEnded
	TraceSpanFailed  = orchestrator.TraceSpanFailed

	PolicyOperationDispatch        = orchestrator.PolicyOperationDispatch
	PolicyOperationBlackboardRead  = orchestrator.PolicyOperationBlackboardRead
	PolicyOperationBlackboardWrite = orchestrator.PolicyOperationBlackboardWrite
	PolicyOperationHandoff         = orchestrator.PolicyOperationHandoff
	PolicyOperationToolCall        = orchestrator.PolicyOperationToolCall
	PolicyOperationAction          = orchestrator.PolicyOperationAction
	PolicyOperationResponseCompose = orchestrator.PolicyOperationResponseCompose
	PolicyOperationResponsePublish = orchestrator.PolicyOperationResponsePublish
)
