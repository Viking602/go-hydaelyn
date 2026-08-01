package model

import "encoding/json"

type ActionAttempt struct {
	AttemptID         string              `json:"attemptId"`
	ActionID          string              `json:"actionId,omitempty"`
	RunID             string              `json:"runId"`
	TaskID            string              `json:"taskId"`
	LeaseID           string              `json:"leaseId,omitempty"`
	ToolName          string              `json:"toolName,omitempty"`
	Status            ActionAttemptStatus `json:"status"`
	IdempotencyKey    string              `json:"idempotencyKey,omitempty"`
	InputHash         string              `json:"inputHash,omitempty"`
	ExternalRequestID string              `json:"externalRequestId,omitempty"`
	ExternalResultRef string              `json:"externalResultRef,omitempty"`
	ToolResult        json.RawMessage     `json:"toolResult,omitempty"`
	RequiresReconcile bool                `json:"requiresReconcile,omitempty"`
}

type ActionAttemptSelector struct {
	RunID             string
	TaskID            string
	ToolName          string
	Statuses          []ActionAttemptStatus
	RequiresReconcile *bool
	Limit             int
}
