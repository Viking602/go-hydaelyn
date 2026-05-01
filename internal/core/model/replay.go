package model

import "time"

type ReplayMode string

const (
	ReplayModeAudit    ReplayMode = "audit"
	ReplayModeRecovery ReplayMode = "recovery"
)

type Projection struct {
	Run         Run               `json:"run"`
	Tasks       map[string]Task   `json:"tasks,omitempty"`
	Messages    []UserMessage     `json:"messages,omitempty"`
	SideEffects ReplaySideEffects `json:"sideEffects"`
}

type ReplaySideEffects struct {
	MailboxDeliveries       int `json:"mailboxDeliveries"`
	UserMessagePublications int `json:"userMessagePublications"`
	ActionExecutions        int `json:"actionExecutions"`
}

type RunTimelineKind string

const (
	RunTimelineKindControl  RunTimelineKind = "control"
	RunTimelineKindWork     RunTimelineKind = "work"
	RunTimelineKindResponse RunTimelineKind = "response"
)

type RunTimelineItem struct {
	Sequence   int             `json:"sequence"`
	RecordedAt time.Time       `json:"recordedAt,omitempty"`
	Kind       RunTimelineKind `json:"kind"`
	RunID      string          `json:"runId"`
	TaskID     string          `json:"taskId,omitempty"`
	Title      string          `json:"title,omitempty"`
	Text       string          `json:"text,omitempty"`
}
