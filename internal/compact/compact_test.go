package compact

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
)

func TestSimpleCompactorNoOpWhenUnderThreshold(t *testing.T) {
	c := &SimpleCompactor{MaxMessages: 10}
	messages := []message.Message{
		{Role: message.RoleSystem, Text: "system"},
		{Role: message.RoleUser, Text: "hello"},
	}

	result, err := c.Compact(context.Background(), messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 messages, got %d", len(result))
	}
}

func TestSimpleCompactorDropsMiddleMessages(t *testing.T) {
	c := &SimpleCompactor{MaxMessages: 4}
	messages := []message.Message{
		{Role: message.RoleSystem, Text: "system"},
		{Role: message.RoleUser, Text: "1"},
		{Role: message.RoleUser, Text: "2"},
		{Role: message.RoleUser, Text: "3"},
		{Role: message.RoleUser, Text: "4"},
		{Role: message.RoleUser, Text: "5"},
	}

	result, err := c.Compact(context.Background(), messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}
	if result[0].Text != "system" {
		t.Errorf("expected first message preserved, got %q", result[0].Text)
	}
	if result[1].Kind != message.KindCompactionSummary {
		t.Errorf("expected compaction summary, got kind %q", result[1].Kind)
	}
	if result[2].Text != "4" || result[3].Text != "5" {
		t.Errorf("expected last messages preserved, got %v", result[2:])
	}
}

func TestLLMCompactorFallsBackToPlaceholderOnStreamError(t *testing.T) {
	c := &LLMCompactor{
		Provider:    &failingProvider{},
		Model:       "test",
		MaxMessages: 4,
	}
	messages := []message.Message{
		{Role: message.RoleSystem, Text: "system"},
		{Role: message.RoleUser, Text: "1"},
		{Role: message.RoleUser, Text: "2"},
		{Role: message.RoleUser, Text: "3"},
		{Role: message.RoleUser, Text: "4"},
	}

	result, err := c.Compact(context.Background(), messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}
	if result[1].Kind != message.KindCompactionSummary {
		t.Errorf("expected compaction summary, got kind %q", result[1].Kind)
	}
	if result[1].Text != "[Compaction summary: 2 earlier messages omitted]" {
		t.Errorf("unexpected fallback summary: %q", result[1].Text)
	}
}

type failingProvider struct{}

func (f *failingProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "fake"}
}

func (f *failingProvider) Stream(_ context.Context, _ provider.Request) (provider.Stream, error) {
	return nil, provider.ErrNotImplemented
}
