package core

import (
	"context"
	"fmt"
	"maps"
	"time"

	commandbus "github.com/Viking602/go-hydaelyn/internal/core/command"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func registerTraceUoWCommandHandlers(runtime *Runtime) {
	commandbus.Register[StartTraceSpanCommand](runtime.commandBus, startTraceSpanHandler{runtime: runtime})
	commandbus.Register[EndTraceSpanCommand](runtime.commandBus, endTraceSpanHandler{})
}

type startTraceSpanHandler struct {
	runtime *Runtime
}

func (h startTraceSpanHandler) Name() string { return StartTraceSpanCommand{}.CommandName() }

func (h startTraceSpanHandler) Handle(ctx context.Context, uow ports.FullUnitOfWork, cmd StartTraceSpanCommand) (any, error) {
	now := time.Now().UTC()
	span := TraceSpan{
		ID:        h.runtime.newID("span"),
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
	if err := uow.Trace().SaveTraceSpan(ctx, span); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: span.RunID, TaskID: span.TaskID, Type: EventTraceSpanStarted, Payload: traceSpanPayload(span), RecordedAt: now}); err != nil {
		return nil, err
	}
	return span, nil
}

type endTraceSpanHandler struct{}

func (endTraceSpanHandler) Name() string { return EndTraceSpanCommand{}.CommandName() }

func (endTraceSpanHandler) Handle(ctx context.Context, uow ports.FullUnitOfWork, cmd EndTraceSpanCommand) (any, error) {
	updater, ok := uow.Trace().(ports.TraceSpanUpdater)
	if !ok {
		return nil, fmt.Errorf("trace store does not implement TraceSpanUpdater: %w", ErrInvalidConfiguration)
	}
	span, err := updater.LoadTraceSpan(ctx, cmd.SpanID)
	if err != nil {
		return nil, err
	}
	span.Status = TraceSpanEnded
	if cmd.Error != "" {
		span.Status = TraceSpanFailed
		span.Error = cmd.Error
	}
	span.EndedAt = time.Now().UTC()
	if err := updater.UpdateTraceSpan(ctx, span); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: span.RunID, TaskID: span.TaskID, Type: EventTraceSpanEnded, Payload: traceSpanPayload(span), RecordedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	return span, nil
}
