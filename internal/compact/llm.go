package compact

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
)

var defaultSummaryPrompt = message.NewText(message.RoleSystem,
	"Summarize the following conversation history into a concise paragraph. "+
		"Preserve key facts, decisions, and context that the assistant will need "+
		"to continue the conversation helpfully. Omit pleasantries and redundant details.")

// LLMCompactor uses a language-model provider to generate a real summary of
// the messages that are dropped from the context window.
type LLMCompactor struct {
	Provider    provider.Driver
	Model       string
	MaxMessages int
}

func (c *LLMCompactor) Compact(ctx context.Context, messages []message.Message) ([]message.Message, error) {
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

	summary, err := c.summarize(ctx, dropped)
	if err != nil {
		// Fall back to the simple placeholder on LLM failure so the
		// conversation can continue.
		summary = fmt.Sprintf("[Compaction summary: %d earlier messages omitted]", len(dropped))
	}

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

func (c *LLMCompactor) summarize(ctx context.Context, dropped []message.Message) (string, error) {
	request := provider.Request{
		Model:    c.Model,
		Messages: append([]message.Message{defaultSummaryPrompt}, dropped...),
	}
	stream, err := c.Provider.Stream(ctx, request)
	if err != nil {
		return "", err
	}
	defer func() { _ = stream.Close() }()

	var summary strings.Builder
	for {
		event, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}
		switch event.Kind {
		case provider.EventTextDelta:
			summary.WriteString(event.Text)
		case provider.EventDone:
			return strings.TrimSpace(summary.String()), nil
		case provider.EventError:
			if event.Err != nil {
				return "", event.Err
			}
			return "", fmt.Errorf("provider emitted error event during compaction")
		}
	}
	return strings.TrimSpace(summary.String()), nil
}
