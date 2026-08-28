package api

import (
	"encoding/json"
	"time"
)

// Command is the public command contract accepted by Runner.ExecuteCommand.
type Command interface {
	CommandName() string
}

type StartRunCommand struct {
	RunID        string
	RootTaskID   string
	Request      string
	AgentVersion string
	Metadata     map[string]string
}

type CreateTaskCommand struct {
	RunID              string
	TaskID             string
	ParentTaskID       string
	Type               TaskType
	Goal               string
	Input              json.RawMessage
	AssignedAgentID    string
	OwnerAgentID       string
	OwnerComponent     string
	AllowsAction       bool
	Tags               []string
	CompletionCriteria []string
	DependsOn          []string
	AwaitMode          AwaitMode
	AwaitQuorum        int
	OnDependencyFailed OnDependencyFailed
	ReadSelectors      []BlackboardSelector
	WriteTargets       []string
	RetryPolicy        RetryPolicy
	PolicyDecisions    []PolicyDecision
	// InputSchema / OutputSchema travel with the created task so the durable
	// worker path can rebuild the agent OutputPolicy from OutputSchema (the
	// store and contract already round-trip these on api.Task).
	InputSchema  json.RawMessage
	OutputSchema json.RawMessage
	// Budget is the per-task loop budget. Like the schemas above it travels
	// with the created task so the durable worker path can enforce the
	// token, tool-call, and step ceilings the agent loop reads off the task.
	Budget         *TaskBudget
	ResourceClaims []ResourceClaimSpec
}

type TransitionRunCommand struct {
	RunID string
	To    RunStatus
}

type TransitionTaskCommand struct {
	RunID  string
	TaskID string
	To     TaskStatus
}

type AdvanceRunCommand struct {
	RunID string
}

type DispatchTaskCommand struct {
	RunID           string
	TaskID          string
	TargetAgentID   string
	TargetComponent string
	// godoc-allow-any: dispatch payloads are host-defined extension data.
	Payload map[string]any
}

type FanOutDispatchTaskCommand struct {
	RunID  string
	TaskID string
	To     Address
	// godoc-allow-any: fan-out payloads are host-defined extension data.
	Payload map[string]any
}

type AckEnvelopeCommand struct {
	EnvelopeID string
	HolderID   string
}

type DeadLetterCommand struct {
	EnvelopeID string
	Reason     string
}

type WriteBlackboardItemCommand struct {
	Item BlackboardItem
}

type AcquireTaskExecutionCommand struct {
	RunID      string
	TaskID     string
	EnvelopeID string
	HolderType HolderType
	HolderID   string
	TTL        time.Duration
}

type HeartbeatTaskExecutionCommand struct {
	LeaseID  string
	HolderID string
	TTL      time.Duration
}

type ReleaseTaskExecutionCommand struct {
	LeaseID  string
	HolderID string
}

type SubmitTypedReportCommand struct {
	RunID       string
	TaskID      string
	LeaseID     string
	HolderType  HolderType
	HolderID    string
	TaskVersion int
	Report      TypedReport
}

type SubmitUserInputCommand struct {
	RunID  string
	TaskID string
	Input  string
}

type ToolInvocation struct {
	RunID       string
	TaskID      string
	LeaseID     string
	HolderType  HolderType
	HolderID    string
	TaskVersion int
	ToolName    string
	// godoc-allow-any: tool input is defined by the selected tool schema.
	Input any
}

type ToolInvocationResult struct {
	ToolName string
	// godoc-allow-any: tool output is defined by the selected tool schema.
	Output   any
	Decision PolicyDecision
}

type HandoffCommand struct {
	RunID          string
	TaskID         string
	FromAgentID    string
	ToAgentID      string
	TaskVersion    int
	HandoffContext string
}

type SubmitResponseOutputCommand struct {
	RunID          string
	TaskID         string
	LeaseID        string
	HolderType     HolderType
	HolderID       string
	TaskVersion    int
	Type           UserMessageType
	Title          string
	Payload        string
	IdempotencyKey string
}

type PublishResponseCommand struct {
	RunID     string
	MessageID string
}

// ReconcileResponsePublicationCommand records the host's determination of
// what happened to a message the runtime left mid-publication. A crash
// between the publish claim and the outcome leaves a message in
// UserMessagePublishing: the output gateway may or may not have delivered
// it, and the runtime will not guess. Only something that can inspect the
// delivery channel — the host — can settle it.
//
// Delivered reports that determination. True marks the message published
// without calling the gateway again; false returns it to the outbox for a
// normal retry. Reason is recorded on the audit event either way.
//
// Re-submitting the same determination is a no-op, so a retried
// reconciliation is safe; submitting the opposite one for a message that has
// already been settled returns ErrIdempotencyConflict.
//
// Operational contract: this command is only for crash residue — a claim
// left behind by a publisher that is no longer running. A publish claim
// carries no holder identity and no expiry, unlike TaskExecutionLease, so
// the runtime cannot tell a dead publisher's claim from a live one and will
// not judge staleness on the host's behalf. Reconciling a message whose
// publisher is still alive races that publisher: both may then write the
// message's terminal status, and marking it published while the gateway call
// is still in flight can leave a real delivery unrecorded or a failed one
// recorded as delivered. The caller MUST establish that no publisher for the
// message survives before invoking this — typically because the process that
// held the claim is known dead. This is an accepted design gap, not an
// oversight: closing it needs a holder and expiry on the claim itself.
type ReconcileResponsePublicationCommand struct {
	RunID     string
	MessageID string
	Delivered bool
	Reason    string
}

type RequestApprovalCommand struct {
	RunID            string
	TaskID           string
	ActionID         string
	RequesterAgentID string
	Reason           string
	RiskSummary      string
	RequestedAction  string
	Metadata         map[string]string
}

type DecideApprovalCommand struct {
	RunID      string
	ApprovalID string
	DecidedBy  string
	Decision   string
	Reason     string
}

type RecoverResumeTokenCommand struct {
	TokenID string
}

type StartActionAttemptCommand struct {
	AttemptID      string
	ActionID       string
	RunID          string
	TaskID         string
	LeaseID        string
	HolderType     HolderType
	HolderID       string
	TaskVersion    int
	ToolName       string
	IdempotencyKey string
	InputHash      string
}

type CompleteActionAttemptCommand struct {
	RunID             string
	TaskID            string
	LeaseID           string
	HolderType        HolderType
	HolderID          string
	TaskVersion       int
	AttemptID         string
	Status            ActionAttemptStatus
	ExternalRequestID string
	ExternalResultRef string
	ToolResult        json.RawMessage
	RequiresReconcile bool
	UsageRecord       *UsageRecord
}

// AppendTaskExecutionEventCommand appends an execution event only while the
// caller still owns the current lease and task version.
type AppendTaskExecutionEventCommand struct {
	RunID        string
	TaskID       string
	LeaseID      string
	HolderType   HolderType
	HolderID     string
	TaskVersion  int
	Event        Event
	UsageRecords []UsageRecord
}

// ResolveActionAttemptCommand records the host's reconciliation decision for an
// attempt whose outcome became unknown after an interruption.
type ResolveActionAttemptCommand struct {
	AttemptID         string
	Status            ActionAttemptStatus
	ExternalResultRef string
	ToolResult        json.RawMessage
}

type StartTraceSpanCommand struct {
	RunID     string
	TaskID    string
	TraceID   string
	ParentID  string
	Name      string
	Component string
	Metadata  map[string]string
}

type EndTraceSpanCommand struct {
	SpanID string
	Error  string
}
