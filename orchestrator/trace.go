package orchestrator

import (
	"context"
	"maps"
	"time"
)

type StartTraceSpanCommand struct {
	RunID     string
	TaskID    string
	TraceID   string
	ParentID  string
	Name      string
	Component string
	Metadata  map[string]string
}

type EndTraceSpanCommand struct {
	SpanID string
	Error  string
}

func (StartTraceSpanCommand) CommandName() string { return "trace.start" }
func (EndTraceSpanCommand) CommandName() string   { return "trace.end" }

func (r *Runtime) StartTraceSpan(_ context.Context, cmd StartTraceSpanCommand) (TraceSpan, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.startTraceSpanLocked(cmd), nil
}

func (r *Runtime) EndTraceSpan(_ context.Context, cmd EndTraceSpanCommand) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for runID, spans := range r.traceSpans {
		for idx, span := range spans {
			if span.ID != cmd.SpanID {
				continue
			}
			span.Status = TraceSpanEnded
			if cmd.Error != "" {
				span.Status = TraceSpanFailed
				span.Error = cmd.Error
			}
			span.EndedAt = time.Now().UTC()
			r.traceSpans[runID][idx] = span
			r.appendEventLocked(span.RunID, span.TaskID, EventTraceSpanEnded, traceSpanPayload(span))
			return nil
		}
	}
	return ErrNotFound
}

func (r *Runtime) TraceSpans(runID string) []TraceSpan {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]TraceSpan{}, r.traceSpans[runID]...)
}

func (r *Runtime) startTraceSpanLocked(cmd StartTraceSpanCommand) TraceSpan {
	now := time.Now().UTC()
	span := TraceSpan{
		ID:        r.newID("span"),
		RunID:     cmd.RunID,
		TaskID:    cmd.TaskID,
		TraceID:   cmd.TraceID,
		ParentID:  cmd.ParentID,
		Name:      cmd.Name,
		Component: cmd.Component,
		Status:    TraceSpanStarted,
		StartedAt: now,
		Metadata:  maps.Clone(cmd.Metadata),
	}
	if span.TraceID == "" {
		span.TraceID = span.ID
	}
	r.traceSpans[span.RunID] = append(r.traceSpans[span.RunID], span)
	r.appendEventLocked(span.RunID, span.TaskID, EventTraceSpanStarted, traceSpanPayload(span))
	return span
}

func (r *Runtime) recordTraceLocked(runID, taskID, name, component string) {
	span := r.startTraceSpanLocked(StartTraceSpanCommand{
		RunID:     runID,
		TaskID:    taskID,
		Name:      name,
		Component: component,
	})
	r.finishTraceSpanLocked(span, nil)
}

func (r *Runtime) finishTraceSpanLocked(span TraceSpan, err error) {
	if span.ID == "" {
		return
	}
	cmd := EndTraceSpanCommand{SpanID: span.ID}
	if err != nil {
		cmd.Error = err.Error()
	}
	for runID, spans := range r.traceSpans {
		for idx, current := range spans {
			if current.ID != span.ID {
				continue
			}
			current.Status = TraceSpanEnded
			if cmd.Error != "" {
				current.Status = TraceSpanFailed
				current.Error = cmd.Error
			}
			current.EndedAt = time.Now().UTC()
			r.traceSpans[runID][idx] = current
			r.appendEventLocked(current.RunID, current.TaskID, EventTraceSpanEnded, traceSpanPayload(current))
			return
		}
	}
}

func traceSpanPayload(span TraceSpan) map[string]any {
	return map[string]any{
		"spanId":    span.ID,
		"runId":     span.RunID,
		"taskId":    span.TaskID,
		"traceId":   span.TraceID,
		"parentId":  span.ParentID,
		"name":      span.Name,
		"component": span.Component,
		"status":    string(span.Status),
		"startedAt": span.StartedAt,
		"endedAt":   span.EndedAt,
		"error":     span.Error,
		"metadata":  maps.Clone(span.Metadata),
	}
}
