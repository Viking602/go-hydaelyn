// Package compact provides conversation compaction strategies for managing
// context window growth. When a session exceeds a threshold, a Compactor can
// replace older messages with a summary so the LLM still receives useful
// context without exceeding token limits.
package compact

import (
	"context"
	"fmt"

	"github.com/Viking602/venat/message"
)

// Compactor summarizes or truncates message histories to fit within a
// context window budget.
type Compactor interface {
	Compact(ctx context.Context, messages []message.Message) ([]message.Message, error)
}

// SimpleCompactor drops middle messages and replaces them with a placeholder
// summary. It never calls an LLM, so it is fast and deterministic.
//
// It always preserves the first complete atomic message unit (typically the
// system prompt), the newest complete units that fit the target, and one
// compaction-summary message. A tool exchange may make the result exceed the
// target because it is never split.
type SimpleCompactor struct {
	MaxMessages int
}

func (c *SimpleCompactor) Compact(_ context.Context, messages []message.Message) ([]message.Message, error) {
	maxMessages := c.MaxMessages
	if maxMessages <= 2 {
		maxMessages = 4
	}

	first, dropped, tail, changed, err := compactionParts(messages, maxMessages)
	if err != nil {
		return messages, err
	}
	if !changed {
		return messages, nil
	}

	summary := fmt.Sprintf("[Compaction summary: %d earlier messages omitted]", len(dropped))
	compacted := make([]message.Message, 0, len(first)+1+len(tail))
	compacted = append(compacted, first...)
	compacted = append(compacted, message.Message{
		Role:       message.RoleSystem,
		Kind:       message.KindCompactionSummary,
		Text:       summary,
		Visibility: message.VisibilityPrivate,
	})
	compacted = append(compacted, tail...)
	return compacted, nil
}

func compactionParts(messages []message.Message, maxMessages int) (first, dropped, tail []message.Message, changed bool, err error) {
	if len(messages) <= maxMessages {
		return messages, nil, nil, false, nil
	}
	if err := message.ValidateCompleteTurns(messages); err != nil {
		return messages, nil, nil, false, err
	}

	prefixEnd, err := message.CompleteTurnBoundary(messages, 1)
	if err != nil {
		return messages, nil, nil, false, err
	}
	if prefixEnd == 0 {
		return messages, nil, nil, false, nil
	}

	keepLast := maxMessages - prefixEnd - 1
	if keepLast < 1 {
		keepLast = 1
	}
	nominalStart := len(messages) - keepLast
	start, err := message.CompleteTurnBoundary(messages, nominalStart)
	if err != nil {
		return messages, nil, nil, false, err
	}
	if start <= prefixEnd {
		return messages, nil, nil, false, nil
	}

	dropped = messages[prefixEnd:start]
	if len(dropped) == 1 && dropped[0].Kind == message.KindCompactionSummary {
		return messages, nil, nil, false, nil
	}
	return messages[:prefixEnd], dropped, messages[start:], true, nil
}
