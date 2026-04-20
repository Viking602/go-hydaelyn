// Package compact provides conversation compaction strategies for managing
// context window growth. When a session exceeds a threshold, a Compactor can
// replace older messages with a summary so the LLM still receives useful
// context without exceeding token limits.
package compact

import (
	"context"
	"fmt"

	"github.com/Viking602/go-hydaelyn/message"
)

// Compactor summarizes or truncates message histories to fit within a
// context window budget.
type Compactor interface {
	Compact(ctx context.Context, messages []message.Message) ([]message.Message, error)
}

// SimpleCompactor drops middle messages and replaces them with a placeholder
// summary. It never calls an LLM, so it is fast and deterministic.
//
// It always preserves the first message (typically the system prompt) and the
// last MaxMessages-1 messages. Everything in between is replaced by a single
// compaction-summary message.
type SimpleCompactor struct {
	MaxMessages int
}

func (c *SimpleCompactor) Compact(_ context.Context, messages []message.Message) ([]message.Message, error) {
	max := c.MaxMessages
	if max <= 2 {
		max = 4
	}
	if len(messages) <= max {
		return messages, nil
	}

	keepFirst := 1 // typically the system prompt
	keepLast := max - keepFirst - 1
	if keepLast < 1 {
		keepLast = 1
	}

	dropped := messages[keepFirst : len(messages)-keepLast]
	summary := fmt.Sprintf("[Compaction summary: %d earlier messages omitted]", len(dropped))

	compacted := make([]message.Message, 0, max)
	compacted = append(compacted, messages[:keepFirst]...)
	compacted = append(compacted, message.Message{
		Role:       message.RoleSystem,
		Kind:       message.KindCompactionSummary,
		Text:       summary,
		Visibility: message.VisibilityPrivate,
	})
	compacted = append(compacted, messages[len(messages)-keepLast:]...)
	return compacted, nil
}
