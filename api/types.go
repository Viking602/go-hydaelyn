package api

import "time"

type ToolEffectType string

const (
	ToolEffectReadOnly           ToolEffectType = "read_only"
	ToolEffectWrite              ToolEffectType = "write"
	ToolEffectExternalSideEffect ToolEffectType = "external_side_effect"
)

// ActionOutcome is the structured outcome of an ActionAttempt. The framework
// owns the action-attempt protocol; the contents are opaque domain payloads
// supplied by the caller.
type ActionOutcome struct {
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

type ActionAttempt struct {
	AttemptID         string              `json:"attemptId"`
	ActionID          string              `json:"actionId,omitempty"`
	RunID             string              `json:"runId"`
	TaskID            string              `json:"taskId"`
	ToolName          string              `json:"toolName,omitempty"`
	Status            ActionAttemptStatus `json:"status"`
	IdempotencyKey    string              `json:"idempotencyKey,omitempty"`
	InputHash         string              `json:"inputHash,omitempty"`
	ExternalRequestID string              `json:"externalRequestId,omitempty"`
	ExternalResultRef string              `json:"externalResultRef,omitempty"`
	RequiresReconcile bool                `json:"requiresReconcile,omitempty"`
}

type AddressKind string

const (
	AddressKindAgent AddressKind = "agent"
	AddressKindRole  AddressKind = "role"
	AddressKindGroup AddressKind = "group"
)

// Address selects one or more agents for fan-out dispatch. Exactly one of
// AgentID, Role, or Group must be set, matching Kind.
type Address struct {
	Kind    AddressKind `json:"kind"`
	AgentID string      `json:"agentId,omitempty"`
	Role    string      `json:"role,omitempty"`
	Group   string      `json:"group,omitempty"`
}

// AgentProfile is the framework-level identity of an agent participating in
// runs. Role and Groups are opaque developer-defined labels used solely for
// fan-out routing.
type AgentProfile struct {
	ID       string            `json:"id"`
	Role     string            `json:"role,omitempty"`
	Groups   []string          `json:"groups,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type BlackboardVisibility string

const (
	BlackboardVisibilityInternal             BlackboardVisibility = "internal"
	BlackboardVisibilityAgentVisible         BlackboardVisibility = "agent_visible"
	BlackboardVisibilityUserVisibleCandidate BlackboardVisibility = "user_visible_candidate"
	BlackboardVisibilityUserVisible          BlackboardVisibility = "user_visible"
)

// BlackboardItemType is the kind of evidence/output a blackboard entry carries.
type BlackboardItemType string

const (
	BlackboardItemClaim          BlackboardItemType = "claim"
	BlackboardItemEvidence       BlackboardItemType = "evidence"
	BlackboardItemFinding        BlackboardItemType = "finding"
	BlackboardItemArtifactRef    BlackboardItemType = "artifact_ref"
	BlackboardItemContext        BlackboardItemType = "context"
	BlackboardItemTaskOutput     BlackboardItemType = "task_output"
	BlackboardItemHandoffContext BlackboardItemType = "handoff_context"
)

type SourceType string

const (
	SourceAgent     SourceType = "agent"
	SourceComponent SourceType = "component"
	SourceTool      SourceType = "tool"
	SourceSystem    SourceType = "system"
)

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

// BlackboardFilter is the subset of BlackboardSelector used to match new items
// for streaming subscribers. RunID is implicit in Subscribe.
type BlackboardFilter = BlackboardSelector

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

type Event struct {
	RunID      string         `json:"runId"`
	TaskID     string         `json:"taskId,omitempty"`
	Sequence   int            `json:"sequence"`
	Type       EventType      `json:"type"`
	Payload    map[string]any `json:"payload,omitempty"`
	RecordedAt time.Time      `json:"recordedAt"`
}

// Flow is a named combination of preset adapters that the Runner uses to
// resolve planner / router / policy / projector behaviour for a Run. Flows
// compose runtime primitives; they cannot bypass any runtime invariant
// (TaskStore, PolicyEngine, TaskExecutionLease, Handoff, ResponseLayer,
// OutputGateway). The bypass concept was removed in v0.8.0 — Runner now
// enforces all primitives unconditionally.
type Flow struct {
	Name            string `json:"name"`
	PlannerPreset   string `json:"plannerPreset,omitempty"`
	RouterPreset    string `json:"routerPreset,omitempty"`
	PolicyPreset    string `json:"policyPreset,omitempty"`
	ProjectorPreset string `json:"projectorPreset,omitempty"`
}

// StartRunResult is the typed result of StartRunCommand.
// Prefer the typed Runner.QueueRun / Runner.StartRun methods over
// ExecuteCommand for compile-time safety; this type is also returned by
// Runner.ExecuteCommand for tools that operate generically over commands.
type StartRunResult struct {
	Run      Run  `json:"run"`
	RootTask Task `json:"rootTask"`
}

// RequestApprovalResult is the typed result of RequestApprovalCommand.
// Prefer the typed Runner.RequestApproval over ExecuteCommand.
type RequestApprovalResult struct {
	Approval ApprovalRequest `json:"approval"`
	Token    ResumeToken     `json:"token"`
}

// AcquireTaskExecutionResult is the typed result of AcquireTaskExecutionCommand.
// Acquired is false when the lease was not granted (already held by another
// holder); in that case Lease is the zero value.
type AcquireTaskExecutionResult struct {
	Lease    TaskExecutionLease `json:"lease"`
	Acquired bool               `json:"acquired"`
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
	// Version is the monotonic CAS source-of-truth for AcquireWithExpectedVersion.
	// Providers MUST increment this on every successful save. v0.8.0+.
	Version uint64 `json:"version,omitempty"`
	// Expiry is the wall-clock deadline at which the lease auto-releases.
	// Distinct from ExpiresAt (which is the deadline of the *task* execution
	// window): Expiry is purely lease-level liveness. v0.8.0+.
	Expiry time.Time `json:"expiry,omitempty"`
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

type HandoffRequest struct {
	HandoffID         string               `json:"handoffId,omitempty"`
	RunID             string               `json:"runId,omitempty"`
	TaskID            string               `json:"taskId,omitempty"`
	FromAgentID       string               `json:"fromAgentId,omitempty"`
	ToAgentID         string               `json:"toAgentId"`
	Reason            string               `json:"reason,omitempty"`
	ContextSummary    string               `json:"contextSummary,omitempty"`
	ContextReferences []string             `json:"contextReferences,omitempty"`
	ContextSelectors  []BlackboardSelector `json:"contextSelectors,omitempty"`
	TaskVersion       int                  `json:"taskVersion,omitempty"`
	Metadata          map[string]string    `json:"metadata,omitempty"`
}

type ApprovalRequest struct {
	ApprovalID       string            `json:"approvalId"`
	RunID            string            `json:"runId"`
	TaskID           string            `json:"taskId"`
	ActionID         string            `json:"actionId,omitempty"`
	RequesterAgentID string            `json:"requesterAgentId,omitempty"`
	Reason           string            `json:"reason,omitempty"`
	RiskSummary      string            `json:"riskSummary,omitempty"`
	RequestedAction  string            `json:"requestedAction,omitempty"`
	ExpiresAt        time.Time         `json:"expiresAt,omitempty"`
	Status           string            `json:"status,omitempty"`
	PayloadRef       string            `json:"payloadRef,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

type ApprovalDecision struct {
	ApprovalID string    `json:"approvalId"`
	DecidedBy  string    `json:"decidedBy,omitempty"`
	Decision   string    `json:"decision"`
	Reason     string    `json:"reason,omitempty"`
	DecidedAt  time.Time `json:"decidedAt,omitempty"`
}

// Intent describes the user's request as interpreted by the runtime pipeline.
type Intent struct {
	RunID   string         `json:"runId"`
	Summary string         `json:"summary,omitempty"`
	Fields  map[string]any `json:"fields,omitempty"`
}

type TodoPlan struct {
	RunID string `json:"runId"`
	Tasks []Task `json:"tasks,omitempty"`
}

type RoutingPlan struct {
	RunID  string      `json:"runId"`
	Routes []TaskRoute `json:"routes,omitempty"`
}

type TaskRoute struct {
	TaskID          string `json:"taskId"`
	TargetAgentID   string `json:"targetAgentId,omitempty"`
	TargetComponent string `json:"targetComponent,omitempty"`
}

type TaskMonitorDecision struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
	Retry    bool   `json:"retry,omitempty"`
}

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

type MessagePolicyChecker func(UserMessage) PolicyDecision

type PolicyOperation string

const (
	PolicyOperationDispatch        PolicyOperation = "dispatch"
	PolicyOperationBlackboardRead  PolicyOperation = "blackboard_read"
	PolicyOperationBlackboardWrite PolicyOperation = "blackboard_write"
	PolicyOperationHandoff         PolicyOperation = "handoff"
	PolicyOperationToolCall        PolicyOperation = "tool_call"
	PolicyOperationAction          PolicyOperation = "action"
	PolicyOperationResponseCompose PolicyOperation = "response_compose"
	PolicyOperationResponsePublish PolicyOperation = "response_publish"
)

type PolicyRequest struct {
	Operation PolicyOperation     `json:"operation"`
	RunID     string              `json:"runId,omitempty"`
	TaskID    string              `json:"taskId,omitempty"`
	Actor     SourceIdentity      `json:"actor,omitempty"`
	Tool      *Tool               `json:"tool,omitempty"`
	Message   *UserMessage        `json:"message,omitempty"`
	Handoff   *HandoffRequest     `json:"handoff,omitempty"`
	Selector  *BlackboardSelector `json:"selector,omitempty"`
	Item      *BlackboardItem     `json:"item,omitempty"`
	Action    *ActionAttempt      `json:"action,omitempty"`
	Metadata  map[string]string   `json:"metadata,omitempty"`
}

type ReplayMode string

const (
	ReplayModeAudit    ReplayMode = "audit"
	ReplayModeRecovery ReplayMode = "recovery"
)

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

type RunTimelineKind string

const (
	RunTimelineKindControl  RunTimelineKind = "control"
	RunTimelineKindWork     RunTimelineKind = "work"
	RunTimelineKindResponse RunTimelineKind = "response"
)

type RunTimelineItem struct {
	Sequence   int             `json:"sequence"`
	RecordedAt time.Time       `json:"recordedAt,omitempty"`
	Kind       RunTimelineKind `json:"kind"`
	RunID      string          `json:"runId"`
	TaskID     string          `json:"taskId,omitempty"`
	Title      string          `json:"title,omitempty"`
	Text       string          `json:"text,omitempty"`
}

type TypedReport struct {
	Status        ReportStatus    `json:"status"`
	Summary       string          `json:"summary,omitempty"`
	Structured    map[string]any  `json:"structured,omitempty"`
	ActionOutcome *ActionOutcome  `json:"actionOutcome,omitempty"`
	Handoff       *HandoffRequest `json:"handoff,omitempty"`
}

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
	AllowsAction       bool                 `json:"allowsAction,omitempty"`
	Tags               []string             `json:"tags,omitempty"`
	CompletionCriteria []string             `json:"completionCriteria,omitempty"`
	DependsOn          []string             `json:"dependsOn,omitempty"`
	AwaitMode          AwaitMode            `json:"awaitMode,omitempty"`
	AwaitQuorum        int                  `json:"awaitQuorum,omitempty"`
	OnDependencyFailed OnDependencyFailed   `json:"onDependencyFailed,omitempty"`
	ReadSelectors      []BlackboardSelector `json:"readSelectors,omitempty"`
	WriteTargets       []string             `json:"writeTargets,omitempty"`
	RetryPolicy        RetryPolicy          `json:"retryPolicy,omitempty"`
	PolicyDecisions    []PolicyDecision     `json:"policyDecisions,omitempty"`
	Result             *TypedReport         `json:"result,omitempty"`
	Error              string               `json:"error,omitempty"`
	CreatedAt          time.Time            `json:"createdAt"`
	UpdatedAt          time.Time            `json:"updatedAt"`
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

// RunSelector filters ListRuns. All set fields AND-combine.
//
// Spec anchor: docs/product-spec/v0.8.0/05-storage.md §"RunSelector".
type RunSelector struct {
	IDs          []string    `json:"ids,omitempty"`
	AgentID      string      `json:"agentId,omitempty"`
	AgentVersion string      `json:"agentVersion,omitempty"`
	Statuses     []RunStatus `json:"statuses,omitempty"`
	Since        time.Time   `json:"since,omitempty"`
	Until        time.Time   `json:"until,omitempty"`
	Limit        int         `json:"limit,omitempty"`
}

// UserMessageSelector filters UserMessageStore.ListPendingFor. All set
// fields AND-combine. The selector is intentionally minimal; providers MAY
// extend their own selectors but the contract test suite only exercises
// these.
//
// Spec anchor: docs/product-spec/v0.8.0/05-storage.md §"Outbox FIFO".
type UserMessageSelector struct {
	RunID     string    `json:"runId,omitempty"`
	Recipient string    `json:"recipient,omitempty"`
	Statuses  []string  `json:"statuses,omitempty"`
	Since     time.Time `json:"since,omitempty"`
	Until     time.Time `json:"until,omitempty"`
	Limit     int       `json:"limit,omitempty"`
}

// ResumeTokenSelector filters ResumeTokenStore.ListPending. Providers MAY
// support cursor-based pagination via Cursor; cursor format is opaque to
// callers.
//
// Spec anchor: docs/product-spec/v0.8.0/05-storage.md §"Resume token enumeration".
type ResumeTokenSelector struct {
	RunID    string    `json:"runId,omitempty"`
	TaskID   string    `json:"taskId,omitempty"`
	Statuses []string  `json:"statuses,omitempty"`
	Since    time.Time `json:"since,omitempty"`
	Until    time.Time `json:"until,omitempty"`
	Limit    int       `json:"limit,omitempty"`
	Cursor   string    `json:"cursor,omitempty"`
}

// AgentSelector filters AgentProfileStore.ListByAgentSelector.
//
// Spec anchor: docs/product-spec/v0.8.0/03-agent-ontology.md.
type AgentSelector struct {
	IDs      []string `json:"ids,omitempty"`
	Roles    []string `json:"roles,omitempty"`
	Groups   []string `json:"groups,omitempty"`
	Statuses []string `json:"statuses,omitempty"`
	Limit    int      `json:"limit,omitempty"`
}

// CapabilitySelector filters CapabilityStore.ListByCapabilitySelector.
//
// Spec anchor: docs/product-spec/v0.8.0/03-agent-ontology.md.
type CapabilitySelector struct {
	Names    []string `json:"names,omitempty"`
	AgentIDs []string `json:"agentIds,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Limit    int      `json:"limit,omitempty"`
}

// UsageRecord is an append-only metering datum captured per task execution.
// The runtime emits one per LLM call, tool call, or other billable unit.
//
// Spec anchor: docs/product-spec/v0.8.0/06-usage-metering.md (detailed
// fields may be added there; this is the v0.8.0 baseline).
type UsageRecord struct {
	ID           string            `json:"id"`
	RunID        string            `json:"runId"`
	TaskID       string            `json:"taskId,omitempty"`
	AgentID      string            `json:"agentId,omitempty"`
	Provider     string            `json:"provider,omitempty"`
	Model        string            `json:"model,omitempty"`
	InputTokens  int               `json:"inputTokens,omitempty"`
	OutputTokens int               `json:"outputTokens,omitempty"`
	ToolCalls    int               `json:"toolCalls,omitempty"`
	DurationMS   int64             `json:"durationMs,omitempty"`
	Credits      int64             `json:"credits,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	CreatedAt    time.Time         `json:"createdAt"`
}

// UsageSelector filters UsageStore.Query.
type UsageSelector struct {
	RunID    string    `json:"runId,omitempty"`
	TaskID   string    `json:"taskId,omitempty"`
	AgentID  string    `json:"agentId,omitempty"`
	Provider string    `json:"provider,omitempty"`
	Since    time.Time `json:"since,omitempty"`
	Until    time.Time `json:"until,omitempty"`
	Limit    int       `json:"limit,omitempty"`
}

// DeadLetterEntry records a TaskEnvelope that exhausted retries and was
// moved to the dead-letter store. Providers MAY support re-queue via the
// DeadLetterStore.Requeue method; this is gated on
// StoreCapabilities.SupportsDeadLetterRequeue.
//
// Spec anchor: docs/product-spec/v0.8.0/04-worker-runtime.md.
type DeadLetterEntry struct {
	ID         string         `json:"id"`
	EnvelopeID string         `json:"envelopeId"`
	RunID      string         `json:"runId"`
	TaskID     string         `json:"taskId,omitempty"`
	Reason     string         `json:"reason,omitempty"`
	Attempts   int            `json:"attempts,omitempty"`
	Envelope   TaskEnvelope   `json:"envelope"`
	Payload    map[string]any `json:"payload,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
}

// DeadLetterSelector filters DeadLetterStore.List.
type DeadLetterSelector struct {
	RunID  string    `json:"runId,omitempty"`
	TaskID string    `json:"taskId,omitempty"`
	Since  time.Time `json:"since,omitempty"`
	Until  time.Time `json:"until,omitempty"`
	Limit  int       `json:"limit,omitempty"`
}

// Capability declares a single unit of work an agent (or system) exposes
// to the runtime — analogous to a typed Tool but at the agent/program
// boundary rather than the model-call boundary. v0.8.0 ships the type
// declaration; AgentProfile + CapabilityStore wiring lands in doc 03.
//
// Spec anchor: docs/product-spec/v0.8.0/03-agent-ontology.md.
type Capability struct {
	Name             string            `json:"name"`
	Version          string            `json:"version,omitempty"`
	Description      string            `json:"description,omitempty"`
	AgentID          string            `json:"agentId,omitempty"`
	InputSchema      map[string]any    `json:"inputSchema,omitempty"`
	OutputSchema     map[string]any    `json:"outputSchema,omitempty"`
	EffectType       ToolEffectType    `json:"effectType,omitempty"`
	RiskLevel        string            `json:"riskLevel,omitempty"`
	Idempotent       bool              `json:"idempotent,omitempty"`
	RequiresApproval bool              `json:"requiresApproval,omitempty"`
	Tags             []string          `json:"tags,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

// AsCapability projects a Tool into the broader Capability shape so it can
// be advertised on a CapabilityManifest, exported to MCP/OpenAPI/CLI, or
// stored via CapabilityStore. The agent that owns the tool is supplied by
// the caller — Tool itself is agent-agnostic. Fields Capability carries
// that Tool does not (description, input/output schema, version) are left
// empty so callers can layer them on top.
//
// The conversion is intentionally lossy in one direction only: round-
// tripping Tool → Capability → Tool drops fields like Description and
// Schemas because they have no Tool counterpart. The reverse direction
// (Capability.AsTool) drops Version, AgentID, schemas, and Tags.
func (t Tool) AsCapability(agentID string) Capability {
	return Capability{
		Name:             t.Name,
		AgentID:          agentID,
		EffectType:       t.EffectType,
		RiskLevel:        t.RiskLevel,
		Idempotent:       t.Idempotent,
		RequiresApproval: t.RequiresActionTask,
		Tags:             append([]string(nil), t.PolicyTags...),
		Metadata:         cloneStringMap(t.Metadata),
	}
}

// AsTool projects a Capability back into a Tool descriptor for use inside
// the model-call boundary (agent.Engine, tool.Bus). Fields Capability
// carries that Tool cannot represent (description, schemas, AgentID,
// version) are dropped; callers needing them should keep the Capability
// alongside the Tool.
func (c Capability) AsTool() Tool {
	return Tool{
		Name:               c.Name,
		EffectType:         c.EffectType,
		RequiresActionTask: c.RequiresApproval,
		RiskLevel:          c.RiskLevel,
		Idempotent:         c.Idempotent,
		PolicyTags:         append([]string(nil), c.Tags...),
		Metadata:           cloneStringMap(c.Metadata),
	}
}

// CapabilityManifest is the declarative bundle a system publishes to
// announce "here is what an agent can ask me to do." A manifest is the
// agent-level analogue of an OpenAPI document: name and version identify
// the manifest itself; each Capability inside is one callable operation.
//
// Manifests are the seed format for the v0.8.0 interop renderers in
// transport/mcp, transport/openapi, and the CLI generator — each turns a
// CapabilityManifest into the format its target ecosystem expects.
type CapabilityManifest struct {
	Name         string            `json:"name"`
	Version      string            `json:"version,omitempty"`
	Description  string            `json:"description,omitempty"`
	Capabilities []Capability      `json:"capabilities"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// cloneStringMap returns a shallow copy of m so callers can mutate the
// result without writing through to the source. Returns nil for nil so
// JSON omitempty behavior is preserved on the round trip.
func cloneStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
