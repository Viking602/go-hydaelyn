package provider

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Viking602/go-hydaelyn/message"
)

var ErrInvalidToolCallArguments = errors.New("invalid tool call arguments")
var ErrDuplicateToolCallID = errors.New("duplicate tool call id")

type NormalizedResponse struct {
	Text       string             `json:"text,omitempty"`
	Thinking   string             `json:"thinking,omitempty"`
	ToolCalls  []message.ToolCall `json:"toolCalls,omitempty"`
	Usage      Usage              `json:"usage,omitempty"`
	StopReason StopReason         `json:"stopReason,omitempty"`
}

func NormalizeEvents(events []Event) (NormalizedResponse, error) {
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
		case EventThinkingDelta:
			response.Thinking += event.Thinking
		case EventToolCall:
			if event.ToolCall == nil {
				continue
			}
			key, builder := ensureToolCallBuilder(builders, &order, idKeys, indexKeys, event.ToolCall.ID, nil, &syntheticSeq)
			if builder.fullSeen {
				return NormalizedResponse{}, fmt.Errorf("%w: %s", ErrDuplicateToolCallID, builder.ID)
			}
			builder.fullSeen = true
			if event.ToolCall.ID != "" {
				builder.ID = event.ToolCall.ID
			}
			if event.ToolCall.Name != "" {
				builder.Name = event.ToolCall.Name
			}
			if len(event.ToolCall.Arguments) > 0 {
				builder.Arguments = string(event.ToolCall.Arguments)
			}
			builders[key] = builder
		case EventToolCallDelta:
			if event.ToolCallDelta == nil {
				continue
			}
			key, builder := ensureToolCallBuilder(builders, &order, idKeys, indexKeys, event.ToolCallDelta.ID, event.ToolCallDelta.Index, &syntheticSeq)
			if event.ToolCallDelta.ID != "" {
				builder.ID = event.ToolCallDelta.ID
			}
			if event.ToolCallDelta.Index != nil {
				idx := *event.ToolCallDelta.Index
				builder.Index = &idx
			}
			if event.ToolCallDelta.Name != "" {
				builder.Name = event.ToolCallDelta.Name
			}
			builder.Arguments += event.ToolCallDelta.ArgumentsDelta
			builders[key] = builder
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
		}
	}

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
