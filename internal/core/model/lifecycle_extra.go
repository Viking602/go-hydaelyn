package model

import "time"

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
