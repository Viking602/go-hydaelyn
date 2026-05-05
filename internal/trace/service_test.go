package trace_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/memory"
	tracesvc "github.com/Viking602/go-hydaelyn/internal/trace"
)

func traceIDGenerator() tracesvc.IDGenerator {
	next := 0
	return func(prefix string) string {
		next++
		return fmt.Sprintf("%s-%d", prefix, next)
	}
}

func TestStartSpanDefaultsTraceIDAndCopiesMetadata(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	metadata := map[string]string{"phase": "dispatch"}
	span, err := tracesvc.StartSpan(ctx, uow, traceIDGenerator(), tracesvc.StartInput{
		RunID:     "run-1",
		TaskID:    "task-1",
		Name:      "mailbox.dispatch",
		Component: "mailbox",
		Metadata:  metadata,
	})
	if err != nil {
		t.Fatalf("StartSpan() error = %v", err)
	}
	metadata["phase"] = "mutated"

	if span.TraceID != span.ID || span.Metadata["phase"] != "dispatch" || span.Status != model.TraceSpanStarted {
		t.Fatalf("StartSpan() contract = %#v", span)
	}
	events, err := uow.Events().ListEvents(ctx, "run-1")
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 1 || events[0].Type != model.EventTraceSpanStarted {
		t.Fatalf("StartSpan() events = %#v", events)
	}
}

func TestEndSpanMarksFailureAndEmitsEvent(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	span, err := tracesvc.StartSpan(ctx, uow, traceIDGenerator(), tracesvc.StartInput{
		RunID:     "run-1",
		TaskID:    "task-1",
		Name:      "policy.authorize",
		Component: "policy",
	})
	if err != nil {
		t.Fatalf("StartSpan() error = %v", err)
	}
	ended, err := tracesvc.EndSpan(ctx, uow, span.ID, "denied")
	if err != nil {
		t.Fatalf("EndSpan() error = %v", err)
	}
	if ended.Status != model.TraceSpanFailed || ended.Error != "denied" || ended.EndedAt.IsZero() {
		t.Fatalf("EndSpan() contract = %#v", ended)
	}
	events, err := uow.Events().ListEvents(ctx, "run-1")
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 2 || events[1].Type != model.EventTraceSpanEnded {
		t.Fatalf("EndSpan() events = %#v", events)
	}
}
