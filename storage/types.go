package storage

import "time"

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
	EventTeamCompleted          EventType = "TeamCompleted"
)

type Event struct {
	RunID      string         `json:"runId"`
	Sequence   int            `json:"sequence"`
	RecordedAt time.Time      `json:"recordedAt,omitempty"`
	Type       EventType      `json:"type"`
	TeamID     string         `json:"teamId,omitempty"`
	TaskID     string         `json:"taskId,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
}
