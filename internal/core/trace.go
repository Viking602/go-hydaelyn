package core

import (
	"context"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
	tracesvc "github.com/Viking602/go-hydaelyn/internal/trace"
)

type (
	StartTraceSpanCommand = tracesvc.StartTraceSpanCommand
	EndTraceSpanCommand   = tracesvc.EndTraceSpanCommand
)

func (r *Runtime) StartTraceSpan(ctx context.Context, cmd StartTraceSpanCommand) (model.TraceSpan, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return model.TraceSpan{}, err
	}
	span, ok := result.(model.TraceSpan)
	if !ok {
		return model.TraceSpan{}, ErrInvalidCommand
	}
	return span, nil
}

func (r *Runtime) EndTraceSpan(ctx context.Context, cmd EndTraceSpanCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runtime) TraceSpans(ctx context.Context, runID string) []model.TraceSpan {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return nil
	}
	defer done()
	spans, err := uow.Trace().ListTraceSpans(ctx, runID)
	if err != nil {
		return nil
	}
	return append([]model.TraceSpan{}, spans...)
}

func registerTraceUoWCommandHandlers(runtime *Runtime) {
	tracesvc.RegisterHandlers(runtime.commandBus, runtime.newID)
}
