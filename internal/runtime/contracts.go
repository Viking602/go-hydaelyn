package runtime

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

type BlackboardStore interface {
	WriteItem(context.Context, BlackboardItem) error
	SelectItems(context.Context, string, BlackboardSelector) ([]BlackboardItem, error)
}

type ResponseOutbox interface {
	QueueMessage(context.Context, UserMessage) error
	PublishMessage(context.Context, string) error
	ListMessages(context.Context, string) ([]UserMessage, error)
}

type UserMessageStore interface {
	QueueMessage(context.Context, UserMessage) error
	LoadMessage(context.Context, string, string) (UserMessage, error)
	UpdateMessage(context.Context, UserMessage) error
	ListMessages(context.Context, string) ([]UserMessage, error)
}

type MailboxOutboxStore interface {
	QueueEnvelope(context.Context, TaskEnvelope) error
	LoadEnvelope(context.Context, string) (TaskEnvelope, error)
	UpdateEnvelope(context.Context, TaskEnvelope) error
	ListEnvelopes(context.Context, string) ([]TaskEnvelope, error)
}

type UnitOfWork interface {
	Runs() RunStore
	Tasks() TaskStore
	Events() EventStore
	Blackboard() BlackboardStore
	MailboxOutbox() MailboxOutboxStore
	UserMessages() UserMessageStore
	Trace() TraceStore
	Commit(context.Context) error
	Rollback(context.Context) error
}

type StoreProvider interface {
	Begin(context.Context) (UnitOfWork, error)
}

type RuntimeCommand interface {
	CommandName() string
}

type PolicyEngine interface {
	Authorize(context.Context, PolicyRequest) (PolicyDecision, error)
}

type OutputGateway interface {
	Publish(context.Context, UserMessage) error
}

type UserTimelineProjector interface {
	ProjectUserTimeline(context.Context, []Event) ([]RunTimelineItem, error)
}

type Projector interface {
	Project(context.Context, []Event) (Projection, error)
}

type IntentAnalyzer interface {
	AnalyzeIntent(context.Context, Run) (Intent, error)
}

type Planner interface {
	CreatePlan(context.Context, Intent) (TodoPlan, error)
}

type PlanValidator interface {
	ValidatePlan(context.Context, TodoPlan) error
}

type TaskRouter interface {
	RouteTasks(context.Context, TodoPlan) (RoutingPlan, error)
}

type Dispatcher interface {
	Dispatch(context.Context, RoutingPlan) ([]TaskEnvelope, error)
}

type TaskMonitor interface {
	Advance(context.Context, Run) error
	DecideDeadLetter(context.Context, TaskEnvelope, string) (TaskMonitorDecision, error)
}

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

type MailboxOutbox = TaskEnvelope

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

type TaskMonitorDecision struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
	Retry    bool   `json:"retry,omitempty"`
}

type PipelineComponents struct {
	IntentAnalyzer IntentAnalyzer
	Planner        Planner
	Validator      PlanValidator
	Router         TaskRouter
	Dispatcher     Dispatcher
	TaskMonitor    TaskMonitor
}
