package storage

import "time"

const PolicyOutcomeEventSchemaVersion = "1.1"

type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
	RunStatusAborted   RunStatus = "aborted"
)

type Run struct {
	ID        string            `json:"id"`
	SessionID string            `json:"sessionId,omitempty"`
	Status    RunStatus         `json:"status"`
	Provider  string            `json:"provider,omitempty"`
	Model     string            `json:"model,omitempty"`
	Error     string            `json:"error,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

type Artifact struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	MIMEType  string            `json:"mimeType,omitempty"`
	Data      []byte            `json:"data,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"createdAt"`
}

type EventType string

const (
	EventTeamStarted            EventType = "TeamStarted"
	EventPlanCreated            EventType = "PlanCreated"
	EventTaskScheduled          EventType = "TaskScheduled"
	EventTaskStarted            EventType = "TaskStarted"
	EventLeaseAcquired          EventType = "LeaseAcquired"
	EventLeaseExpired           EventType = "LeaseExpired"
	EventTaskInputsMaterialized EventType = "TaskInputsMaterialized"
	EventToolCalled             EventType = "ToolCalled"
	EventTaskCompleted          EventType = "TaskCompleted"
	EventTaskOutputsPublished   EventType = "TaskOutputsPublished"
	EventTaskFailed             EventType = "TaskFailed"
	EventStaleWriteRejected     EventType = "StaleWriteRejected"
	EventVerifierPassed         EventType = "VerifierPassed"
	EventVerifierBlocked        EventType = "VerifierBlocked"
	EventTaskCancelled          EventType = "TaskCancelled"
	EventCancelled              EventType = EventTaskCancelled
	EventSynthesisCommitted     EventType = "SynthesisCommitted"
	EventCheckpointSaved        EventType = "CheckpointSaved"
	EventApprovalRequested      EventType = "ApprovalRequested"
	EventPolicyOutcome          EventType = "PolicyOutcome"
	EventTeamCompleted          EventType = "TeamCompleted"
	EventMailboxSent            EventType = "MailboxSent"
	EventMailboxDelivered       EventType = "MailboxDelivered"
	EventMailboxAcked           EventType = "MailboxAcked"
	EventMailboxNacked          EventType = "MailboxNacked"
	EventMailboxExpired         EventType = "MailboxExpired"
	EventMailboxDead            EventType = "MailboxDead"
)

type PolicyOutcomeEvent struct {
	SchemaVersion string                 `json:"schemaVersion"`
	Layer         string                 `json:"layer,omitempty"`
	Stage         string                 `json:"stage,omitempty"`
	Operation     string                 `json:"operation,omitempty"`
	Action        string                 `json:"action,omitempty"`
	Policy        string                 `json:"policy"`
	Outcome       string                 `json:"outcome,omitempty"`
	Severity      string                 `json:"severity,omitempty"`
	Message       string                 `json:"message,omitempty"`
	Blocking      bool                   `json:"blocking,omitempty"`
	RunID         string                 `json:"runId,omitempty"`
	TeamID        string                 `json:"teamId,omitempty"`
	TaskID        string                 `json:"taskId,omitempty"`
	AgentID       string                 `json:"agentId,omitempty"`
	Reference     string                 `json:"reference,omitempty"`
	Attempt       int                    `json:"attempt,omitempty"`
	Timestamp     time.Time              `json:"timestamp"`
	Evidence      *PolicyOutcomeEvidence `json:"evidence,omitempty"`
}

type PolicyOutcomeEvidence struct {
	EventSequences []int             `json:"eventSequences,omitempty"`
	Excerpt        string            `json:"excerpt,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

type Event struct {
	RunID      string         `json:"runId"`
	Sequence   int            `json:"sequence"`
	RecordedAt time.Time      `json:"recordedAt,omitempty"`
	Type       EventType      `json:"type"`
	TeamID     string         `json:"teamId,omitempty"`
	TaskID     string         `json:"taskId,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
}
