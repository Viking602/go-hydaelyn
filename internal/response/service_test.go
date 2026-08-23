package response_test

import (
	"context"
	"testing"

	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/memory"
	"github.com/Viking602/venat/internal/response"
)

func TestRedactionHelpersRemoveEmailAndInternalTrace(t *testing.T) {
	payload := "send to owner@example.com\ninternal trace: span-1\nkeep this"
	redacted := response.HideInternalTrace(response.RedactUserPayload(payload))
	if redacted != "send to [redacted-email]\nkeep this" {
		t.Fatalf("redacted payload = %q", redacted)
	}
}

func TestCriticalContextItemDefaultsSource(t *testing.T) {
	item := response.CriticalContextItem("id-1", "run-1", "task-1", model.SourceIdentity{}, "key", "payload")
	if item.Source.Type != model.SourceSystem || item.Source.ID != "orchestrator" {
		t.Fatalf("default source = %#v", item.Source)
	}
	if item.Type != model.BlackboardItemContext || item.Key != "key" || item.Content != "payload" {
		t.Fatalf("item = %#v", item)
	}
}

func TestAppendBlackboardWrittenEvent(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	item := response.CriticalContextItem("id-1", "run-1", "task-1", model.SourceIdentity{Type: model.SourceAgent, ID: "a"}, "key", "payload")
	if err := response.AppendBlackboardWrittenEvent(ctx, uow, item); err != nil {
		t.Fatalf("AppendBlackboardWrittenEvent() error = %v", err)
	}
	events, err := uow.Events().ListEvents(ctx, "run-1")
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 1 || events[0].Type != model.EventBlackboardItemWritten {
		t.Fatalf("events = %#v", events)
	}
}
