package trace

import (
	"context"
	"maps"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/ports"
)

type IDGenerator func(prefix string) string

type StartInput struct {
	RunID     string
	TaskID    string
	TraceID   string
	ParentID  string
	Name      string
	Component string
	Metadata  map[string]string
}

func StartSpan(ctx context.Context, uow ports.UnitOfWork, newID IDGenerator, input StartInput) (api.TraceSpan, error) {
	now := time.Now().UTC()
	span := api.TraceSpan{
		ID:        newID("span"),
		RunID:     input.RunID,
		TaskID:    input.TaskID,
		TraceID:   input.TraceID,
		ParentID:  input.ParentID,
		Name:      input.Name,
		Component: input.Component,
		Status:    api.TraceSpanStarted,
		StartedAt: now,
		Metadata:  maps.Clone(input.Metadata),
	}
	if span.TraceID == "" {
		span.TraceID = span.ID
	}
	if err := uow.Trace().SaveTraceSpan(ctx, span); err != nil {
		return api.TraceSpan{}, err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: span.RunID, TaskID: span.TaskID, Type: api.EventTraceSpanStarted, Payload: Payload(span), RecordedAt: now}); err != nil {
		return api.TraceSpan{}, err
	}
	return span, nil
}

func EndSpan(ctx context.Context, uow ports.UnitOfWork, spanID, spanError string) (api.TraceSpan, error) {
	span, err := uow.Trace().LoadTraceSpan(ctx, spanID)
	if err != nil {
		return api.TraceSpan{}, err
	}
	span.Status = api.TraceSpanEnded
	if spanError != "" {
		span.Status = api.TraceSpanFailed
		span.Error = spanError
	}
	span.EndedAt = time.Now().UTC()
	if err := uow.Trace().UpdateTraceSpan(ctx, span); err != nil {
		return api.TraceSpan{}, err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: span.RunID, TaskID: span.TaskID, Type: api.EventTraceSpanEnded, Payload: Payload(span), RecordedAt: time.Now().UTC()}); err != nil {
		return api.TraceSpan{}, err
	}
	return span, nil
}

func Payload(span api.TraceSpan) map[string]any {
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
