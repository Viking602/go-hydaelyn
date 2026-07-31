package compact

import (
	"context"
	"reflect"
	"testing"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
)

func TestSimpleCompactorNoOpWhenUnderThreshold(t *testing.T) {
	c := &SimpleCompactor{MaxMessages: 10}
	messages := []message.Message{
		{Role: message.RoleSystem, Text: "system"},
		{
			Role:      message.RoleAssistant,
			ToolCalls: []message.ToolCall{{ID: "incomplete", Name: "tool"}},
		},
	}

	result, err := c.Compact(context.Background(), messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(result, messages) {
		t.Fatalf("no-op compaction changed or validated the under-threshold history: %#v", result)
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

func TestSimpleCompactorPreservesCompleteToolTurns(t *testing.T) {
	c := &SimpleCompactor{MaxMessages: 4}
	messages := []message.Message{
		message.NewText(message.RoleSystem, "system"),
		message.NewText(message.RoleUser, "old-1"),
		message.NewText(message.RoleUser, "old-2"),
		message.NewText(message.RoleUser, "old-3"),
	}
	messages = append(messages, completeToolTurn("call-1")...)

	result, err := c.Compact(context.Background(), messages)
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if err := message.ValidateCompleteTurns(result); err != nil {
		t.Fatalf("compacted history contains an incomplete tool turn: %v", err)
	}
	if len(result) != 4 {
		t.Fatalf("len(result) = %d, want 4", len(result))
	}
	if len(result[2].ToolCalls) != 1 || result[2].ToolCalls[0].ID != "call-1" {
		t.Fatalf("assistant tool call was not retained: %#v", result)
	}
	if result[3].ToolResult == nil || result[3].ToolResult.ToolCallID != "call-1" {
		t.Fatalf("matching tool result was not retained: %#v", result)
	}
}

func TestSimpleCompactorAtomicTurnSoftensLimit(t *testing.T) {
	c := &SimpleCompactor{MaxMessages: 4}
	messages := []message.Message{
		message.NewText(message.RoleSystem, "system"),
		message.NewText(message.RoleUser, "old"),
	}
	messages = append(messages, completeToolTurn("call-1", "call-2")...)

	result, err := c.Compact(context.Background(), messages)
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if err := message.ValidateCompleteTurns(result); err != nil {
		t.Fatalf("compacted history contains an incomplete tool turn: %v", err)
	}
	if len(result) != 5 {
		t.Fatalf("len(result) = %d, want 5 because the newest atomic turn exceeds the target", len(result))
	}
	if len(result[2].ToolCalls) != 2 {
		t.Fatalf("newest atomic tool turn was not retained intact: %#v", result)
	}
}

func TestSimpleCompactorDropsOlderStraddlingTurn(t *testing.T) {
	c := &SimpleCompactor{MaxMessages: 5}
	messages := []message.Message{message.NewText(message.RoleSystem, "system")}
	messages = append(messages, completeToolTurn("old-call-1", "old-call-2")...)
	messages = append(messages,
		message.NewText(message.RoleUser, "new-user"),
		message.NewText(message.RoleAssistant, "new-assistant"),
	)

	result, err := c.Compact(context.Background(), messages)
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if err := message.ValidateCompleteTurns(result); err != nil {
		t.Fatalf("compacted history contains an incomplete tool turn: %v", err)
	}
	if len(result) != 4 {
		t.Fatalf("len(result) = %d, want 4 after dropping the full older tool turn", len(result))
	}
	if result[1].Text != "[Compaction summary: 3 earlier messages omitted]" {
		t.Fatalf("summary = %q, want the complete three-message tool turn to be omitted", result[1].Text)
	}
	if result[2].Text != "new-user" || result[3].Text != "new-assistant" {
		t.Fatalf("newer units were not retained: %#v", result)
	}
}

func TestSimpleCompactorOversizedTurnIsIdempotent(t *testing.T) {
	c := &SimpleCompactor{MaxMessages: 4}
	summary := message.Message{
		Role:       message.RoleSystem,
		Kind:       message.KindCompactionSummary,
		Text:       "existing summary",
		Visibility: message.VisibilityPrivate,
	}
	messages := []message.Message{
		message.NewText(message.RoleSystem, "system"),
		summary,
	}
	messages = append(messages, completeToolTurn("call-1", "call-2", "call-3")...)

	first, err := c.Compact(context.Background(), messages)
	if err != nil {
		t.Fatalf("first Compact() error = %v", err)
	}
	second, err := c.Compact(context.Background(), first)
	if err != nil {
		t.Fatalf("second Compact() error = %v", err)
	}
	if !reflect.DeepEqual(first, messages) || !reflect.DeepEqual(second, messages) {
		t.Fatalf("repeated compaction changed a history whose only droppable unit was the old summary:\nfirst: %#v\nsecond: %#v", first, second)
	}

	withNewerUnit := append(append([]message.Message{}, second...), message.NewText(message.RoleUser, "newer"))
	compacted, err := c.Compact(context.Background(), withNewerUnit)
	if err != nil {
		t.Fatalf("Compact() after newer unit error = %v", err)
	}
	if reflect.DeepEqual(compacted, withNewerUnit) {
		t.Fatal("older oversized turn remained protected after a newer complete unit arrived")
	}
	if err := message.ValidateCompleteTurns(compacted); err != nil {
		t.Fatalf("compacted history contains an incomplete tool turn: %v", err)
	}
	if compacted[len(compacted)-1].Text != "newer" {
		t.Fatalf("newest unit was not retained: %#v", compacted)
	}
}

func TestLLMCompactorPreservesCompleteToolTurns(t *testing.T) {
	c := &LLMCompactor{
		Provider:    &failingProvider{},
		Model:       "test",
		MaxMessages: 4,
	}
	messages := []message.Message{
		message.NewText(message.RoleSystem, "system"),
		message.NewText(message.RoleUser, "old-1"),
		message.NewText(message.RoleUser, "old-2"),
		message.NewText(message.RoleUser, "old-3"),
	}
	messages = append(messages, completeToolTurn("call-1")...)

	result, err := c.Compact(context.Background(), messages)
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if err := message.ValidateCompleteTurns(result); err != nil {
		t.Fatalf("compacted history contains an incomplete tool turn: %v", err)
	}
	if len(result) != 4 {
		t.Fatalf("len(result) = %d, want 4", len(result))
	}
	if len(result[2].ToolCalls) != 1 || result[3].ToolResult == nil {
		t.Fatalf("LLM compactor did not retain the complete newest tool turn: %#v", result)
	}
}

func completeToolTurn(ids ...string) []message.Message {
	calls := make([]message.ToolCall, len(ids))
	for index, id := range ids {
		calls[index] = message.ToolCall{ID: id, Name: "tool"}
	}
	turn := []message.Message{{
		Role:      message.RoleAssistant,
		ToolCalls: calls,
	}}
	for _, id := range ids {
		turn = append(turn, message.NewToolResult(message.ToolResult{ToolCallID: id, Name: "tool", Content: "ok"}))
	}
	return turn
}

type failingProvider struct{}

func (f *failingProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "fake"}
}

func (f *failingProvider) Stream(_ context.Context, _ provider.Request) (provider.Stream, error) {
	return nil, provider.ErrNotImplemented
}
