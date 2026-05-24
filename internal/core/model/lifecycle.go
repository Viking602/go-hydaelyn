package model

import "time"

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
	Version     uint64      `json:"version,omitempty"`
	Expiry      time.Time   `json:"expiry,omitempty"`
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
