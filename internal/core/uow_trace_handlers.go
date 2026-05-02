package core

import (
	"context"

	commandbus "github.com/Viking602/go-hydaelyn/internal/core/command"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
	tracesvc "github.com/Viking602/go-hydaelyn/internal/trace"
)

func registerTraceUoWCommandHandlers(runtime *Runtime) {
	commandbus.Register[StartTraceSpanCommand](runtime.commandBus, startTraceSpanHandler{runtime: runtime})
	commandbus.Register[EndTraceSpanCommand](runtime.commandBus, endTraceSpanHandler{})
}

type startTraceSpanHandler struct {
	runtime *Runtime
}

func (h startTraceSpanHandler) Name() string { return StartTraceSpanCommand{}.CommandName() }

func (h startTraceSpanHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd StartTraceSpanCommand) (any, error) {
	return tracesvc.StartSpan(ctx, uow, h.runtime.newID, tracesvc.StartInput{
		RunID:     cmd.RunID,
		TaskID:    cmd.TaskID,
		TraceID:   cmd.TraceID,
		ParentID:  cmd.ParentID,
		Name:      cmd.Name,
		Component: cmd.Component,
		Metadata:  cmd.Metadata,
	})
}

type endTraceSpanHandler struct{}

func (endTraceSpanHandler) Name() string { return EndTraceSpanCommand{}.CommandName() }

func (endTraceSpanHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd EndTraceSpanCommand) (any, error) {
	return tracesvc.EndSpan(ctx, uow, cmd.SpanID, cmd.Error)
}
