package model

import "time"

type Run struct {
	ID           string            `json:"id"`
	Status       RunStatus         `json:"status"`
	Request      string            `json:"request,omitempty"`
	RootTaskID   string            `json:"rootTaskId,omitempty"`
	AgentVersion string            `json:"agentVersion,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	CreatedAt    time.Time         `json:"createdAt"`
	UpdatedAt    time.Time         `json:"updatedAt"`
}
