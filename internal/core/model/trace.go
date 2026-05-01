package model

import "time"

type TraceSpanStatus string

const (
	TraceSpanStarted TraceSpanStatus = "started"
	TraceSpanEnded   TraceSpanStatus = "ended"
	TraceSpanFailed  TraceSpanStatus = "failed"
)

type TraceSpan struct {
	ID        string            `json:"spanId"`
	RunID     string            `json:"runId,omitempty"`
	TaskID    string            `json:"taskId,omitempty"`
	TraceID   string            `json:"traceId,omitempty"`
	ParentID  string            `json:"parentId,omitempty"`
	Name      string            `json:"name"`
	Component string            `json:"component,omitempty"`
	Status    TraceSpanStatus   `json:"status"`
	StartedAt time.Time         `json:"startedAt"`
	EndedAt   time.Time         `json:"endedAt,omitempty"`
	Error     string            `json:"error,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}
