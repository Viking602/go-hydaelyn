package core

import (
	"context"

	"github.com/Viking602/venat/api"
	tracesvc "github.com/Viking602/venat/internal/trace"
)

type (
	StartTraceSpanCommand = tracesvc.StartTraceSpanCommand
	EndTraceSpanCommand   = tracesvc.EndTraceSpanCommand
)

func (r *Runtime) StartTraceSpan(ctx context.Context, cmd StartTraceSpanCommand) (api.TraceSpan, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return api.TraceSpan{}, err
	}
	span, ok := result.(api.TraceSpan)
	if !ok {
		return api.TraceSpan{}, ErrInvalidCommand
	}
	return span, nil
}

func (r *Runtime) EndTraceSpan(ctx context.Context, cmd EndTraceSpanCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runtime) TraceSpans(ctx context.Context, runID string) []api.TraceSpan {
	spans, err := r.ListTraceSpans(ctx, runID)
	if err != nil {
		return nil
	}
	return spans
}

func (r *Runtime) ListTraceSpans(ctx context.Context, runID string) ([]api.TraceSpan, error) {
	uow, err := r.beginWriteUoW(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = uow.Rollback(ctx)
		}
	}()
	decision, err := r.authorizeUoW(ctx, uow, api.PolicyRequest{
		Operation: api.PolicyOperationTraceRead,
		RunID:     runID,
	})
	if err != nil {
		if isCommitCommandError(err) {
			if commitErr := uow.Commit(ctx); commitErr != nil {
				return nil, commitErr
			}
			committed = true
		}
		return nil, err
	}
	spans, err := uow.Trace().ListTraceSpans(ctx, runID)
	if err != nil {
		return nil, err
	}
	spans, err = r.enforceTraceSpansUoW(ctx, uow, runID, decision, spans)
	if err != nil {
		if isCommitCommandError(err) {
			if commitErr := uow.Commit(ctx); commitErr != nil {
				return nil, commitErr
			}
			committed = true
		}
		return nil, err
	}
	if err := uow.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return spans, nil
}

func registerTraceUoWCommandHandlers(runtime *Runtime) {
	tracesvc.RegisterHandlers(runtime.commandBus, runtime.newID)
}
