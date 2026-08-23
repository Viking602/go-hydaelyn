package provider

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Viking602/venat/message"
)

var (
	ErrInvalidToolCallArguments = errors.New("invalid tool call arguments")
	ErrDuplicateToolCallID      = errors.New("duplicate tool call id")
	ErrMissingTerminalEvent     = errors.New("provider stream ended without a terminal event")
	ErrMultipleTerminalEvents   = errors.New("provider stream returned multiple terminal events")
	ErrEventAfterTerminal       = errors.New("provider stream returned an event after termination")
)

type NormalizedResponse struct {
	Content  []message.ContentPart `json:"content,omitempty"`
	Text     string                `json:"text,omitempty"`
	Thinking string                `json:"thinking,omitempty"`
	// Signature is the opaque thinking-block signature accumulated from
	// signature_delta events; empty for providers that do not sign reasoning.
	Signature string `json:"signature,omitempty"`
	// RedactedThinking is the opaque payload of a redacted_thinking block, if
	// the provider emitted one.
	RedactedThinking string             `json:"redactedThinking,omitempty"`
	ToolCalls        []message.ToolCall `json:"toolCalls,omitempty"`
	Usage            Usage              `json:"usage,omitempty"`
	StopReason       StopReason         `json:"stopReason,omitempty"`
	ProviderState    json.RawMessage    `json:"providerState,omitempty"`
}

// NormalizeEvents folds one complete provider stream and requires exactly one
// terminal event at the end.
func NormalizeEvents(events []Event) (NormalizedResponse, error) {
	return normalizeEvents(events, true)
}

// NormalizePartialEvents folds events already observed from an interrupted
// stream. A terminal event is optional, but if present it must still be unique
// and last.
func NormalizePartialEvents(events []Event) (NormalizedResponse, error) {
	return normalizeEvents(events, false)
}

func normalizeEvents(events []Event, requireTerminal bool) (NormalizedResponse, error) {
	if err := validateTerminalSequence(events, false); err != nil {
		return NormalizedResponse{}, err
	}
	response := NormalizedResponse{}
	builders := map[string]*toolCallBuilder{}
	order := make([]string, 0)
	idKeys := map[string]string{}
	indexKeys := map[int]string{}
	syntheticSeq := 0

	for _, event := range events {
		response.Usage = response.Usage.Add(event.Usage)
		switch event.Kind {
		case EventTextDelta:
			response.Text += event.Text
			response.Content = appendTextContent(response.Content, contentKindForPhase(event.TextPhase), event.Text, "")
		case EventThinkingDelta:
			response.Thinking += event.Thinking
			response.Content = appendTextContent(response.Content, message.ContentReasoning, event.Thinking, event.Signature)
			if event.Signature != "" {
				response.Signature = event.Signature
			}
			if event.RedactedThinking != "" {
				response.RedactedThinking = event.RedactedThinking
				response.Content = append(response.Content, message.ContentPart{
					Kind: message.ContentRedactedReasoning,
					Data: []byte(event.RedactedThinking),
				})
			}
		case EventToolCall:
			if err := applyToolCallEvent(event.ToolCall, builders, &order, idKeys, indexKeys, &syntheticSeq); err != nil {
				return NormalizedResponse{}, err
			}
		case EventToolCallDelta:
			applyToolCallDeltaEvent(event.ToolCallDelta, builders, &order, idKeys, indexKeys, &syntheticSeq)
		case EventError:
			if event.Err != nil {
				return NormalizedResponse{}, event.Err
			}
			return NormalizedResponse{}, errors.New("provider stream returned error event")
		case EventDone:
			response.StopReason = event.StopReason
			if event.Usage != (Usage{}) {
				response.Usage = event.Usage
			}
			if len(event.ProviderState) > 0 {
				response.ProviderState = event.ProviderState
			}
		}
	}
	if requireTerminal {
		if err := validateTerminalSequence(events, true); err != nil {
			return NormalizedResponse{}, err
		}
	}

	return finalizeToolCalls(response, order, builders)
}

func validateTerminalSequence(events []Event, requireTerminal bool) error {
	terminal := -1
	for index, event := range events {
		if event.Kind != EventDone && event.Kind != EventError {
			if terminal >= 0 {
				return fmt.Errorf("%w: terminal index %d, event index %d", ErrEventAfterTerminal, terminal, index)
			}
			continue
		}
		if terminal >= 0 {
			return fmt.Errorf("%w: indexes %d and %d", ErrMultipleTerminalEvents, terminal, index)
		}
		terminal = index
	}
	if requireTerminal && terminal < 0 {
		return ErrMissingTerminalEvent
	}
	return nil
}

func contentKindForPhase(phase TextPhase) message.ContentKind {
	switch phase {
	case TextPhaseCommentary:
		return message.ContentCommentary
	case TextPhaseFinalAnswer:
		return message.ContentFinalAnswer
	default:
		return message.ContentText
	}
}

func appendTextContent(parts []message.ContentPart, kind message.ContentKind, text, signature string) []message.ContentPart {
	if text == "" && signature == "" {
		return parts
	}
	if len(parts) > 0 && parts[len(parts)-1].Kind == kind {
		parts[len(parts)-1].Text += text
		if signature != "" {
			parts[len(parts)-1].Signature = signature
		}
		return parts
	}
	return append(parts, message.ContentPart{Kind: kind, Text: text, Signature: signature})
}

// applyToolCallEvent records a complete tool call. Returns an error when the
// builder for the same id has already been finalized (duplicate id case).
func applyToolCallEvent(call *message.ToolCall, builders map[string]*toolCallBuilder, order *[]string, idKeys map[string]string, indexKeys map[int]string, syntheticSeq *int) error {
	if call == nil {
		return nil
	}
	key, builder := ensureToolCallBuilder(builders, order, idKeys, indexKeys, call.ID, nil, syntheticSeq)
	if builder.fullSeen {
		return fmt.Errorf("%w: %s", ErrDuplicateToolCallID, builder.ID)
	}
	builder.fullSeen = true
	if call.ID != "" {
		builder.ID = call.ID
	}
	if call.Name != "" {
		builder.Name = call.Name
	}
	if len(call.Arguments) > 0 {
		builder.Arguments = string(call.Arguments)
	}
	builders[key] = builder
	return nil
}

// applyToolCallDeltaEvent merges a partial tool-call delta into its builder.
func applyToolCallDeltaEvent(delta *ToolCallDelta, builders map[string]*toolCallBuilder, order *[]string, idKeys map[string]string, indexKeys map[int]string, syntheticSeq *int) {
	if delta == nil {
		return
	}
	key, builder := ensureToolCallBuilder(builders, order, idKeys, indexKeys, delta.ID, delta.Index, syntheticSeq)
	if delta.ID != "" {
		builder.ID = delta.ID
	}
	if delta.Index != nil {
		idx := *delta.Index
		builder.Index = &idx
	}
	if delta.Name != "" {
		builder.Name = delta.Name
	}
	builder.Arguments += delta.ArgumentsDelta
	builders[key] = builder
}

// finalizeToolCalls walks the accumulated builders in arrival order, validates
// the JSON arguments, and appends them to response.ToolCalls.
func finalizeToolCalls(response NormalizedResponse, order []string, builders map[string]*toolCallBuilder) (NormalizedResponse, error) {
	for _, key := range order {
		builder := builders[key]
		if builder == nil {
			continue
		}
		if builder.Arguments != "" && !json.Valid([]byte(builder.Arguments)) {
			return NormalizedResponse{}, fmt.Errorf("%w: %s", ErrInvalidToolCallArguments, toolCallBuilderLabel(key, builder))
		}
		response.ToolCalls = append(response.ToolCalls, message.ToolCall{
			ID:        builder.ID,
			Name:      builder.Name,
			Arguments: []byte(builder.Arguments),
		})
	}
	return response, nil
}

type toolCallBuilder struct {
	ID        string
	Index     *int
	Name      string
	Arguments string
	fullSeen  bool
}

func ensureToolCallBuilder(builders map[string]*toolCallBuilder, order *[]string, idKeys map[string]string, indexKeys map[int]string, id string, index *int, syntheticSeq *int) (string, *toolCallBuilder) {
	if id != "" {
		if key, ok := idKeys[id]; ok {
			builder := builders[key]
			if index != nil {
				indexKeys[*index] = key
			}
			return key, builder
		}
	}
	if index != nil {
		if key, ok := indexKeys[*index]; ok {
			builder := builders[key]
			if id != "" {
				idKeys[id] = key
			}
			return key, builder
		}
	}

	var key string
	switch {
	case id != "":
		key = "id:" + id
	case index != nil:
		key = fmt.Sprintf("index:%d", *index)
	default:
		key = fmt.Sprintf("tool-call-%d", *syntheticSeq)
		*syntheticSeq++
	}
	if builder, ok := builders[key]; ok {
		if id != "" {
			idKeys[id] = key
		}
		if index != nil {
			indexKeys[*index] = key
		}
		return key, builder
	}

	builder := &toolCallBuilder{ID: id}
	if id != "" {
		idKeys[id] = key
	}
	if index != nil {
		idx := *index
		builder.Index = &idx
		indexKeys[idx] = key
	}
	builders[key] = builder
	*order = append(*order, key)
	return key, builder
}

func toolCallBuilderLabel(key string, builder *toolCallBuilder) string {
	if builder == nil {
		return key
	}
	if builder.ID != "" {
		return builder.ID
	}
	if builder.Index != nil {
		return fmt.Sprintf("index:%d", *builder.Index)
	}
	return key
}
