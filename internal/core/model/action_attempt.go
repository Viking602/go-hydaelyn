package model

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
