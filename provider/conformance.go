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
			key, builder, err := ensureToolCallBuilder(builders, order, event.ToolCall.ID)
			if err != nil {
				return NormalizedResponse{}, err
			}
			if !contains(order, key) {
				order = append(order, key)
			}
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
			key, builder, err := ensureToolCallBuilder(builders, order, event.ToolCallDelta.ID)
			if err != nil {
				return NormalizedResponse{}, err
			}
			if !contains(order, key) {
				order = append(order, key)
			}
			if event.ToolCallDelta.ID != "" {
				builder.ID = event.ToolCallDelta.ID
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
			return NormalizedResponse{}, fmt.Errorf("%w: %s", ErrInvalidToolCallArguments, builder.ID)
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
	Name      string
	Arguments string
	fullSeen  bool
}

func ensureToolCallBuilder(builders map[string]*toolCallBuilder, order []string, id string) (string, *toolCallBuilder, error) {
	key := id
	if key == "" {
		key = fmt.Sprintf("tool-call-%d", len(order))
	}
	if builder, ok := builders[key]; ok {
		return key, builder, nil
	}
	builder := &toolCallBuilder{ID: id}
	builders[key] = builder
	return key, builder, nil
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
