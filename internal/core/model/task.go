package model

import (
	"encoding/json"
	"time"
)

type TaskBudget struct {
	MaxTokens    int64         `json:"maxTokens,omitempty"`
	MaxWallClock time.Duration `json:"maxWallClock,omitempty"`
	MaxToolCalls int           `json:"maxToolCalls,omitempty"`
	MaxSteps     int           `json:"maxSteps,omitempty"`
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
	Budget             *TaskBudget          `json:"budget,omitempty"`
	InputSchema        json.RawMessage      `json:"inputSchema,omitempty"`
	OutputSchema       json.RawMessage      `json:"outputSchema,omitempty"`
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
