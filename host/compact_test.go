package host

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/compact"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/session"
)

type countingProvider struct {
	seenLen int
}

func (c *countingProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "counting"}
}

func (c *countingProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	c.seenLen = len(request.Messages)
	last := request.Messages[len(request.Messages)-1]
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: last.Text},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

func TestPromptWithoutCompactionPassesFullHistory(t *testing.T) {
	prov := &countingProvider{}
	runner := New(Config{})
	runner.RegisterProvider("counting", prov)

	sess, err := runner.CreateSession(context.Background(), session.CreateParams{})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// Seed the session with 5 messages
	for i := 0; i < 5; i++ {
		_, _ = runner.appendSessionMessages(context.Background(), sess.ID,
			message.NewText(message.RoleUser, "msg"))
	}

	_, err = runner.Prompt(context.Background(), PromptRequest{
		SessionID: sess.ID,
		Provider:  "counting",
		Model:     "test",
		Messages:  []message.Message{message.NewText(message.RoleUser, "go")},
	})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}

	if prov.seenLen != 6 {
		t.Errorf("expected 6 messages without compaction, got %d", prov.seenLen)
	}
}

func TestPromptWithCompactionReducesMessages(t *testing.T) {
	prov := &countingProvider{}
	runner := New(Config{
		Compactor:        &compact.SimpleCompactor{MaxMessages: 4},
		CompactThreshold: 4,
	})
	runner.RegisterProvider("counting", prov)

	sess, err := runner.CreateSession(context.Background(), session.CreateParams{})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// Seed the session with 5 messages
	for i := 0; i < 5; i++ {
		_, _ = runner.appendSessionMessages(context.Background(), sess.ID,
			message.NewText(message.RoleUser, "msg"))
	}

	_, err = runner.Prompt(context.Background(), PromptRequest{
		SessionID: sess.ID,
		Provider:  "counting",
		Model:     "test",
		Messages:  []message.Message{message.NewText(message.RoleUser, "go")},
	})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}

	// 5 stored + 1 new = 6 total; compacted to MaxMessages=4
	if prov.seenLen != 4 {
		t.Errorf("expected 4 compacted messages, got %d", prov.seenLen)
	}
}

func TestCompactMessagesFailOpen(t *testing.T) {
	runner := New(Config{
		Compactor:        &failingCompactor{},
		CompactThreshold: 2,
	})

	messages := []message.Message{
		message.NewText(message.RoleSystem, "system"),
		message.NewText(message.RoleUser, "a"),
		message.NewText(message.RoleUser, "b"),
		message.NewText(message.RoleUser, "c"),
	}

	result := runner.compactMessages(context.Background(), messages)
	if len(result) != 4 {
		t.Errorf("expected 4 messages when compactor fails, got %d", len(result))
	}
}

type failingCompactor struct{}

func (f *failingCompactor) Compact(_ context.Context, _ []message.Message) ([]message.Message, error) {
	return nil, provider.ErrNotImplemented
}
