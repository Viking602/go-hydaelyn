package trace

import (
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
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

func StartSpan(ctx context.Context, uow ports.UnitOfWork, newID IDGenerator, input StartInput) (model.TraceSpan, error) {
	now := time.Now().UTC()
	span := model.TraceSpan{
		ID:        newID("span"),
		RunID:     input.RunID,
		TaskID:    input.TaskID,
		TraceID:   input.TraceID,
		ParentID:  input.ParentID,
		Name:      input.Name,
		Component: input.Component,
		Status:    model.TraceSpanStarted,
		StartedAt: now,
		Metadata:  maps.Clone(input.Metadata),
	}
	if span.TraceID == "" {
		span.TraceID = span.ID
	}
	if err := uow.Trace().SaveTraceSpan(ctx, span); err != nil {
		return model.TraceSpan{}, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: span.RunID, TaskID: span.TaskID, Type: model.EventTraceSpanStarted, Payload: Payload(span), RecordedAt: now}); err != nil {
		return model.TraceSpan{}, err
	}
	return span, nil
}

func EndSpan(ctx context.Context, uow ports.UnitOfWork, spanID, spanError string) (model.TraceSpan, error) {
	updater, ok := uow.Trace().(ports.TraceSpanUpdater)
	if !ok {
		return model.TraceSpan{}, fmt.Errorf("trace store does not implement TraceSpanUpdater: %w", model.ErrInvalidConfiguration)
	}
	span, err := updater.LoadTraceSpan(ctx, spanID)
	if err != nil {
		return model.TraceSpan{}, err
	}
	span.Status = model.TraceSpanEnded
	if spanError != "" {
		span.Status = model.TraceSpanFailed
		span.Error = spanError
	}
	span.EndedAt = time.Now().UTC()
	if err := updater.UpdateTraceSpan(ctx, span); err != nil {
		return model.TraceSpan{}, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: span.RunID, TaskID: span.TaskID, Type: model.EventTraceSpanEnded, Payload: Payload(span), RecordedAt: time.Now().UTC()}); err != nil {
		return model.TraceSpan{}, err
	}
	return span, nil
}

func Payload(span model.TraceSpan) map[string]any {
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
