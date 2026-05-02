package core

import (
	"context"

	tracesvc "github.com/Viking602/go-hydaelyn/internal/trace"
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

func (r *Runtime) StartTraceSpan(ctx context.Context, cmd StartTraceSpanCommand) (TraceSpan, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return TraceSpan{}, err
	}
	span, ok := result.(TraceSpan)
	if !ok {
		return TraceSpan{}, ErrInvalidCommand
	}
	return span, nil
}

func (r *Runtime) EndTraceSpan(ctx context.Context, cmd EndTraceSpanCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runtime) TraceSpans(runID string) []TraceSpan {
	ctx := context.Background()
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return nil
	}
	defer done()
	spans, err := uow.Trace().ListTraceSpans(ctx, runID)
	if err != nil {
		return nil
	}
	return append([]TraceSpan{}, spans...)
}

func traceSpanPayload(span TraceSpan) map[string]any {
	return tracesvc.Payload(span)
}
