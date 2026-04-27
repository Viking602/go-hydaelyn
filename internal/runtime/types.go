package runtime

import (
	"errors"
	"time"

	"github.com/Viking602/go-hydaelyn/message"
)

var (
	ErrNotFound                = errors.New("orchestrator: not found")
	ErrTerminalState           = errors.New("orchestrator: terminal state")
	ErrStaleTaskVersion        = errors.New("orchestrator: stale task version")
	ErrLeaseHolderMismatch     = errors.New("orchestrator: lease holder mismatch")
	ErrLeaseNotActive          = errors.New("orchestrator: lease not active")
	ErrOwnerMismatch           = errors.New("orchestrator: owner mismatch")
	ErrActionTaskRequired      = errors.New("orchestrator: action task required")
	ErrActionReconcileRequired = errors.New("orchestrator: action reconcile required")
	ErrResponseTaskRequired    = errors.New("orchestrator: response task required")
	ErrPolicyDenied            = errors.New("orchestrator: policy denied")
	ErrPolicyObligationFailed  = errors.New("orchestrator: policy obligation failed")
	ErrFlowBypass              = errors.New("orchestrator: flow bypasses runtime primitives")
	ErrHandoffCycle            = errors.New("orchestrator: handoff owner cycle")
	ErrHandoffDepthExceeded    = errors.New("orchestrator: handoff depth exceeded")
	ErrInvalidCommand          = errors.New("orchestrator: invalid command")
	ErrInvalidTransition       = errors.New("orchestrator: invalid state transition")
	ErrCompletionCriteriaUnmet = errors.New("orchestrator: completion criteria unmet")
	ErrDependencyUnmet         = errors.New("orchestrator: dependency unmet")
)

type RunStatus string

const (
	RunStatusCreated           RunStatus = "created"
	RunStatusPlanning          RunStatus = "planning"
	RunStatusValidating        RunStatus = "validating"
	RunStatusRouting           RunStatus = "routing"
	RunStatusDispatching       RunStatus = "dispatching"
	RunStatusRunning           RunStatus = "running"
	RunStatusSynthesizing      RunStatus = "synthesizing"
	RunStatusReviewing         RunStatus = "reviewing"
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

type TaskType string

const (
	TaskTypeWorker    TaskType = "worker"
	TaskTypeSynthesis TaskType = "synthesis"
	TaskTypeReview    TaskType = "review"
	TaskTypeAction    TaskType = "action"
	TaskTypeResponse  TaskType = "response"
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

type ToolEffectType = message.ToolEffectType

const (
	ToolEffectReadOnly           = message.ToolEffectReadOnly
	ToolEffectWrite              = message.ToolEffectWrite
	ToolEffectExternalSideEffect = message.ToolEffectExternalSideEffect
)

type BlackboardVisibility string

const (
	BlackboardVisibilityInternal             BlackboardVisibility = "internal"
	BlackboardVisibilityAgentVisible         BlackboardVisibility = "agent_visible"
	BlackboardVisibilityUserVisibleCandidate BlackboardVisibility = "user_visible_candidate"
	BlackboardVisibilityUserVisible          BlackboardVisibility = "user_visible"
)

type BlackboardItemType string

const (
	BlackboardItemClaim          BlackboardItemType = "claim"
	BlackboardItemEvidence       BlackboardItemType = "evidence"
	BlackboardItemFinding        BlackboardItemType = "finding"
	BlackboardItemArtifactRef    BlackboardItemType = "artifact_ref"
	BlackboardItemContext        BlackboardItemType = "context"
	BlackboardItemTaskOutput     BlackboardItemType = "task_output"
	BlackboardItemHandoffContext BlackboardItemType = "handoff_context"
	BlackboardItemSynthesis      BlackboardItemType = "synthesis"
	BlackboardItemReviewResult   BlackboardItemType = "review_result"
	BlackboardItemActionResult   BlackboardItemType = "action_result"
)

type SourceType string

const (
	SourceAgent     SourceType = "agent"
	SourceComponent SourceType = "component"
	SourceTool      SourceType = "tool"
	SourceSystem    SourceType = "system"
)

type PolicyEffect string

const (
	PolicyEffectAllow           PolicyEffect = "allow"
	PolicyEffectDeny            PolicyEffect = "deny"
	PolicyEffectRequireApproval PolicyEffect = "require_approval"
	PolicyEffectPause           PolicyEffect = "pause"
	PolicyEffectAbort           PolicyEffect = "abort"
)

type ObligationKind string

const (
	ObligationRedactFields           ObligationKind = "redact_fields"
	ObligationSelectorOnly           ObligationKind = "selector_only"
	ObligationRequireHumanApproval   ObligationKind = "require_human_approval"
	ObligationHideInternalTrace      ObligationKind = "hide_internal_trace"
	ObligationMaskToolOutput         ObligationKind = "mask_tool_output"
	ObligationRestrictHandoffContext ObligationKind = "restrict_handoff_context"
)

type UserMessageStatus string

const (
	UserMessageComposed  UserMessageStatus = "composed"
	UserMessageQueued    UserMessageStatus = "queued"
	UserMessagePublished UserMessageStatus = "published"
	UserMessageFailed    UserMessageStatus = "failed"
	UserMessageCancelled UserMessageStatus = "cancelled"
)

type UserMessageType string

const (
	UserMessageTypeFinalAnswer          UserMessageType = "final_answer"
	UserMessageTypeProgressUpdate       UserMessageType = "progress_update"
	UserMessageTypeApprovalRequest      UserMessageType = "approval_request"
	UserMessageTypeClarificationRequest UserMessageType = "clarification_request"
	UserMessageTypeExecutionResult      UserMessageType = "execution_result"
	UserMessageTypeErrorNotice          UserMessageType = "error_notice"
	UserMessageTypeBlockedNotice        UserMessageType = "blocked_notice"
)

type EventType string

const (
	EventRunStarted                  EventType = "RunStarted"
	EventRunStatusChanged            EventType = "RunStatusChanged"
	EventIntentAnalyzed              EventType = "IntentAnalyzed"
	EventPlanCreated                 EventType = "PlanCreated"
	EventPlanValidated               EventType = "PlanValidated"
	EventRoutingPlanCreated          EventType = "RoutingPlanCreated"
	EventTaskCreated                 EventType = "TaskCreated"
	EventTaskDispatched              EventType = "TaskDispatched"
	EventEnvelopeAcked               EventType = "EnvelopeAcked"
	EventEnvelopeDeadLettered        EventType = "EnvelopeDeadLettered"
	EventTaskExecutionAcquired       EventType = "TaskExecutionAcquired"
	EventTaskExecutionHeartbeat      EventType = "TaskExecutionHeartbeat"
	EventTaskExecutionReleased       EventType = "TaskExecutionReleased"
	EventTypedReportSubmitted        EventType = "TypedReportSubmitted"
	EventTaskCompleted               EventType = "TaskCompleted"
	EventTaskFailed                  EventType = "TaskFailed"
	EventTaskBlocked                 EventType = "TaskBlocked"
	EventTaskPaused                  EventType = "TaskPaused"
	EventUserInputSubmitted          EventType = "UserInputSubmitted"
	EventResumeTokenCreated          EventType = "ResumeTokenCreated"
	EventActionReconcileRequired     EventType = "ActionReconcileRequired"
	EventActionAttemptStarted        EventType = "ActionAttemptStarted"
	EventActionAttemptUpdated        EventType = "ActionAttemptUpdated"
	EventBlackboardItemWritten       EventType = "BlackboardItemWritten"
	EventHandoffRequested            EventType = "HandoffRequested"
	EventHandoffApplied              EventType = "HandoffApplied"
	EventHandoffEnvelopeQueued       EventType = "HandoffEnvelopeQueued"
	EventApprovalRequested           EventType = "ApprovalRequested"
	EventApprovalDecided             EventType = "ApprovalDecided"
	EventTaskOwnerChanged            EventType = "TaskOwnerChanged"
	EventPolicyObligationFailed      EventType = "PolicyObligationFailed"
	EventResponseTaskCreated         EventType = "ResponseTaskCreated"
	EventSystemResponseBypassAudited EventType = "SystemResponseBypassAudited"
	EventUserMessageComposed         EventType = "UserMessageComposed"
	EventUserMessagePolicyChecked    EventType = "UserMessagePolicyChecked"
	EventUserMessageQueued           EventType = "UserMessageQueued"
	EventResponsePublished           EventType = "ResponsePublished"
	EventResponsePublishFailed       EventType = "ResponsePublishFailed"
	EventTaskMonitorDecision         EventType = "TaskMonitorDecision"
	EventMailboxRetryScheduled       EventType = "MailboxRetryScheduled"
	EventTraceSpanStarted            EventType = "TraceSpanStarted"
	EventTraceSpanEnded              EventType = "TraceSpanEnded"
)

type ReplayMode string

const (
	ReplayModeAudit    ReplayMode = "audit"
	ReplayModeRecovery ReplayMode = "recovery"
)

type Run struct {
	ID         string            `json:"id"`
	Status     RunStatus         `json:"status"`
	Request    string            `json:"request,omitempty"`
	RootTaskID string            `json:"rootTaskId,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	CreatedAt  time.Time         `json:"createdAt"`
	UpdatedAt  time.Time         `json:"updatedAt"`
}

type Task struct {
	ID                 string               `json:"taskId"`
	RunID              string               `json:"runId"`
	ParentTaskID       string               `json:"parentTaskId,omitempty"`
	Type               TaskType             `json:"type"`
	Goal               string               `json:"goal,omitempty"`
	AssignedAgentID    string               `json:"assignedAgentId,omitempty"`
	OwnerAgentID       string               `json:"ownerAgentId,omitempty"`
	OwnerComponent     string               `json:"ownerComponent,omitempty"`
	Status             TaskStatus           `json:"status"`
	Version            int                  `json:"version"`
	Attempts           int                  `json:"attempts,omitempty"`
	HandoffCount       int                  `json:"handoffCount,omitempty"`
	OwnerHistory       []string             `json:"ownerHistory,omitempty"`
	CompletionCriteria []string             `json:"completionCriteria,omitempty"`
	DependsOn          []string             `json:"dependsOn,omitempty"`
	ReadSelectors      []BlackboardSelector `json:"readSelectors,omitempty"`
	WriteTargets       []string             `json:"writeTargets,omitempty"`
	RetryPolicy        RetryPolicy          `json:"retryPolicy,omitempty"`
	PolicyDecisions    []PolicyDecision     `json:"policyDecisions,omitempty"`
	Result             *TypedReport         `json:"result,omitempty"`
	Error              string               `json:"error,omitempty"`
	CreatedAt          time.Time            `json:"createdAt"`
	UpdatedAt          time.Time            `json:"updatedAt"`
}

type TaskExecutionLease struct {
	ID          string      `json:"leaseId"`
	RunID       string      `json:"runId"`
	TaskID      string      `json:"taskId"`
	EnvelopeID  string      `json:"envelopeId,omitempty"`
	HolderType  HolderType  `json:"holderType"`
	HolderID    string      `json:"holderId"`
	TaskVersion int         `json:"taskVersion"`
	AcquiredAt  time.Time   `json:"acquiredAt"`
	ExpiresAt   time.Time   `json:"expiresAt"`
	HeartbeatAt time.Time   `json:"heartbeatAt"`
	Status      LeaseStatus `json:"status"`
}

type TypedReport struct {
	Status       ReportStatus    `json:"status"`
	Summary      string          `json:"summary,omitempty"`
	Structured   map[string]any  `json:"structured,omitempty"`
	ActionResult *ActionResult   `json:"actionResult,omitempty"`
	Handoff      *HandoffRequest `json:"handoff,omitempty"`
}

type ActionResult struct {
	AttemptID         string              `json:"attemptId"`
	ResultID          string              `json:"resultId,omitempty"`
	ActionID          string              `json:"actionId,omitempty"`
	RunID             string              `json:"runId,omitempty"`
	TaskID            string              `json:"taskId,omitempty"`
	Status            ActionAttemptStatus `json:"status"`
	Summary           string              `json:"summary,omitempty"`
	Output            string              `json:"output,omitempty"`
	ArtifactRefs      []string            `json:"artifactRefs,omitempty"`
	RollbackAvailable bool                `json:"rollbackAvailable,omitempty"`
	ExternalResultRef string              `json:"externalResultRef,omitempty"`
	CreatedAt         time.Time           `json:"createdAt,omitempty"`
	Error             string              `json:"error,omitempty"`
}

type Tool struct {
	Name               string            `json:"name"`
	EffectType         ToolEffectType    `json:"effectType"`
	RequiresActionTask bool              `json:"requiresActionTask,omitempty"`
	RiskLevel          string            `json:"riskLevel,omitempty"`
	Idempotent         bool              `json:"idempotent,omitempty"`
	Timeout            time.Duration     `json:"timeout,omitempty"`
	RetryPolicy        RetryPolicy       `json:"retryPolicy,omitempty"`
	PolicyTags         []string          `json:"policyTags,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
}

type RetryPolicy struct {
	MaxAttempts int           `json:"maxAttempts,omitempty"`
	Backoff     time.Duration `json:"backoff,omitempty"`
}

type SourceIdentity struct {
	Type SourceType `json:"type"`
	ID   string     `json:"id"`
}

type BlackboardItem struct {
	ID           string               `json:"id"`
	RunID        string               `json:"runId"`
	TaskID       string               `json:"taskId,omitempty"`
	Type         BlackboardItemType   `json:"type,omitempty"`
	Source       SourceIdentity       `json:"source"`
	Content      string               `json:"content,omitempty"`
	Confidence   float64              `json:"confidence,omitempty"`
	EvidenceRefs []string             `json:"evidenceRefs,omitempty"`
	ArtifactRefs []string             `json:"artifactRefs,omitempty"`
	Visibility   BlackboardVisibility `json:"visibility"`
	Version      int                  `json:"version,omitempty"`
	Key          string               `json:"key,omitempty"`
	Payload      string               `json:"payload,omitempty"`
	CreatedAt    time.Time            `json:"createdAt"`
}

type BlackboardSelector struct {
	RunID       string               `json:"runId,omitempty"`
	TaskID      string               `json:"taskId,omitempty"`
	ItemTypes   []BlackboardItemType `json:"itemTypes,omitempty"`
	SourceTypes []SourceType         `json:"sourceTypes,omitempty"`
	SourceIDs   []string             `json:"sourceIds,omitempty"`
	// Deprecated: use SourceTypes: [SourceAgent] plus SourceIDs instead.
	SourceAgentIDs []string             `json:"sourceAgentIds,omitempty"`
	Visibility     BlackboardVisibility `json:"visibility,omitempty"`
	Tags           []string             `json:"tags,omitempty"`
	SinceVersion   int                  `json:"sinceVersion,omitempty"`
	Limit          int                  `json:"limit,omitempty"`
	Keys           []string             `json:"keys,omitempty"`
}

type TaskEnvelope struct {
	ID              string               `json:"envelopeId"`
	RunID           string               `json:"runId"`
	TaskID          string               `json:"taskId"`
	TodoID          string               `json:"todoId,omitempty"`
	From            string               `json:"from,omitempty"`
	Type            string               `json:"type,omitempty"`
	TargetAgentID   string               `json:"targetAgentId,omitempty"`
	TargetComponent string               `json:"targetComponent,omitempty"`
	Payload         map[string]any       `json:"payload,omitempty"`
	ReadSelectors   []BlackboardSelector `json:"readSelectors,omitempty"`
	WriteTargets    []string             `json:"writeTargets,omitempty"`
	TraceID         string               `json:"traceId,omitempty"`
	TaskVersion     int                  `json:"taskVersion,omitempty"`
	Deadline        time.Time            `json:"deadline,omitempty"`
	RetryPolicy     RetryPolicy          `json:"retryPolicy,omitempty"`
	Status          string               `json:"status"`
	Attempts        int                  `json:"attempts,omitempty"`
	NextRetryAt     time.Time            `json:"nextRetryAt,omitempty"`
	CreatedAt       time.Time            `json:"createdAt"`
	DeliveredAt     time.Time            `json:"deliveredAt,omitempty"`
}

type PolicyDecision struct {
	DecisionID       string             `json:"decisionId"`
	Effect           PolicyEffect       `json:"effect"`
	Reason           string             `json:"reason,omitempty"`
	Obligations      []PolicyObligation `json:"obligations,omitempty"`
	Redactions       []string           `json:"redactions,omitempty"`
	ApprovalRequired bool               `json:"approvalRequired,omitempty"`
	ExpiresAt        time.Time          `json:"expiresAt,omitempty"`
	Metadata         map[string]string  `json:"metadata,omitempty"`
}

type PolicyObligation struct {
	Kind   ObligationKind `json:"kind"`
	Target string         `json:"target,omitempty"`
}

type UserMessage struct {
	ID             string            `json:"messageId"`
	RunID          string            `json:"runId"`
	TaskID         string            `json:"taskId"`
	Type           UserMessageType   `json:"type,omitempty"`
	Title          string            `json:"title,omitempty"`
	Payload        string            `json:"payload"`
	Status         UserMessageStatus `json:"status"`
	IdempotencyKey string            `json:"idempotencyKey,omitempty"`
	PublishedAt    time.Time         `json:"publishedAt,omitempty"`
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt"`
}

type ResumeToken struct {
	TokenID          string            `json:"tokenId"`
	RunID            string            `json:"runId"`
	TaskID           string            `json:"taskId,omitempty"`
	ApprovalID       string            `json:"approvalId,omitempty"`
	ExpiresAt        time.Time         `json:"expiresAt"`
	ResumeCommand    string            `json:"resumeCommand,omitempty"`
	ResumeRunState   RunStatus         `json:"resumeRunState,omitempty"`
	ResumeTaskState  TaskStatus        `json:"resumeTaskState,omitempty"`
	ResumePayloadRef string            `json:"resumePayloadRef,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

type Flow struct {
	Name                     string `json:"name"`
	PlannerPreset            string `json:"plannerPreset,omitempty"`
	RouterPreset             string `json:"routerPreset,omitempty"`
	PolicyPreset             string `json:"policyPreset,omitempty"`
	ProjectorPreset          string `json:"projectorPreset,omitempty"`
	BypassTaskStore          bool   `json:"bypassTaskStore,omitempty"`
	BypassPolicyEngine       bool   `json:"bypassPolicyEngine,omitempty"`
	BypassTaskExecutionLease bool   `json:"bypassTaskExecutionLease,omitempty"`
	BypassHandoff            bool   `json:"bypassHandoff,omitempty"`
	BypassResponseLayer      bool   `json:"bypassResponseLayer,omitempty"`
	BypassOutputGateway      bool   `json:"bypassOutputGateway,omitempty"`
}

type Event struct {
	RunID      string         `json:"runId"`
	TaskID     string         `json:"taskId,omitempty"`
	Sequence   int            `json:"sequence"`
	Type       EventType      `json:"type"`
	Payload    map[string]any `json:"payload,omitempty"`
	RecordedAt time.Time      `json:"recordedAt"`
}

type Projection struct {
	Run         Run               `json:"run"`
	Tasks       map[string]Task   `json:"tasks,omitempty"`
	Messages    []UserMessage     `json:"messages,omitempty"`
	SideEffects ReplaySideEffects `json:"sideEffects"`
}

type ReplaySideEffects struct {
	MailboxDeliveries       int `json:"mailboxDeliveries"`
	UserMessagePublications int `json:"userMessagePublications"`
	ActionExecutions        int `json:"actionExecutions"`
}

type MessagePolicyChecker func(UserMessage) PolicyDecision

type TraceSpanStatus string

const (
	TraceSpanStarted TraceSpanStatus = "started"
	TraceSpanEnded   TraceSpanStatus = "ended"
	TraceSpanFailed  TraceSpanStatus = "failed"
)

type TraceSpan struct {
	ID        string            `json:"spanId"`
	RunID     string            `json:"runId,omitempty"`
	TaskID    string            `json:"taskId,omitempty"`
	TraceID   string            `json:"traceId,omitempty"`
	ParentID  string            `json:"parentId,omitempty"`
	Name      string            `json:"name"`
	Component string            `json:"component,omitempty"`
	Status    TraceSpanStatus   `json:"status"`
	StartedAt time.Time         `json:"startedAt"`
	EndedAt   time.Time         `json:"endedAt,omitempty"`
	Error     string            `json:"error,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}
