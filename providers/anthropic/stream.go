package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/providers/shared"
)

type requestBody struct {
	Model         string             `json:"model"`
	MaxTokens     int                `json:"max_tokens"`
	Messages      []anthropicMessage `json:"messages"`
	Tools         []anthropicTool    `json:"tools,omitempty"`
	Stream        bool               `json:"stream"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Thinking      *thinkingOptions   `json:"thinking,omitempty"`
}

type thinkingOptions struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicTool struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	InputSchema message.JSONSchema `json:"input_schema"`
}

type eventEnvelope struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type streamState struct {
	reader     *shared.Reader
	pending    []provider.Event
	finished   bool
	usage      provider.Usage
	stopReason provider.StopReason
	toolCalls  map[int]provider.ToolCallDelta
}

func (d Driver) Stream(ctx context.Context, request provider.Request) (provider.Stream, error) {
	apiKey := d.config.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("anthropic api key is required")
	}
	maxTokens := d.config.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	body, err := json.Marshal(requestBody{
		Model:         request.Model,
		MaxTokens:     maxTokens,
		Messages:      toAnthropicMessages(request.Messages),
		Tools:         toAnthropicTools(request.Tools),
		Stream:        true,
		StopSequences: request.StopSequences,
		Thinking:      thinkingFromBudget(request.ThinkingBudget),
	})
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(d.config.BaseURL, "/") + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", d.config.Version)
	req.Header.Set("Content-Type", "application/json")
	client := d.config.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		payload, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic api error: %s", strings.TrimSpace(string(payload)))
	}
	return &anthropicStream{
		body: resp.Body,
		state: streamState{
			reader:    shared.NewReader(resp.Body),
			toolCalls: map[int]provider.ToolCallDelta{},
		},
	}, nil
}

type anthropicStream struct {
	body  io.ReadCloser
	state streamState
}

func (s *anthropicStream) Recv() (provider.Event, error) {
	for {
		if len(s.state.pending) > 0 {
			event := s.state.pending[0]
			s.state.pending = s.state.pending[1:]
			return event, nil
		}
		if s.state.finished {
			return provider.Event{}, io.EOF
		}
		current, err := s.state.reader.Next()
		if err != nil {
			return provider.Event{}, err
		}
		var parsed eventEnvelope
		if err := json.Unmarshal([]byte(current.Data), &parsed); err != nil {
			return provider.Event{}, err
		}
		switch parsed.Type {
		case "message_start":
			s.state.usage.InputTokens = parsed.Usage.InputTokens
		case "content_block_start":
			if parsed.ContentBlock.Type == "tool_use" {
				s.state.toolCalls[parsed.Index] = provider.ToolCallDelta{
					ID:   parsed.ContentBlock.ID,
					Name: parsed.ContentBlock.Name,
				}
				current := s.state.toolCalls[parsed.Index]
				s.state.pending = append(s.state.pending, provider.Event{
					Kind:          provider.EventToolCallDelta,
					ToolCallDelta: &current,
				})
			}
		case "content_block_delta":
			if parsed.Delta.Type == "text_delta" {
				s.state.pending = append(s.state.pending, provider.Event{
					Kind: provider.EventTextDelta,
					Text: parsed.Delta.Text,
				})
			}
			if parsed.Delta.Type == "thinking_delta" {
				s.state.pending = append(s.state.pending, provider.Event{
					Kind:     provider.EventThinkingDelta,
					Thinking: parsed.Delta.Thinking,
				})
			}
			if parsed.Delta.Type == "input_json_delta" {
				current := s.state.toolCalls[parsed.Index]
				current.ArgumentsDelta = parsed.Delta.PartialJSON
				s.state.toolCalls[parsed.Index] = current
				s.state.pending = append(s.state.pending, provider.Event{
					Kind:          provider.EventToolCallDelta,
					ToolCallDelta: &current,
				})
			}
		case "message_delta":
			s.state.usage.OutputTokens = parsed.Usage.OutputTokens
			s.state.usage.TotalTokens = s.state.usage.InputTokens + parsed.Usage.OutputTokens
			s.state.stopReason = mapAnthropicStopReason(parsed.Delta.StopReason)
		case "message_stop":
			s.state.finished = true
			return provider.Event{
				Kind:       provider.EventDone,
				Usage:      s.state.usage,
				StopReason: s.state.stopReason,
			}, nil
		case "error":
			return provider.Event{}, fmt.Errorf("anthropic stream error")
		}
	}
}

func (s *anthropicStream) Close() error {
	return s.body.Close()
}

func toAnthropicMessages(messages []message.Message) []anthropicMessage {
	items := make([]anthropicMessage, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case message.RoleTool:
			if msg.ToolResult != nil {
				items = append(items, anthropicMessage{Role: "user", Content: msg.ToolResult.Content})
			}
		case message.RoleSystem:
			items = append(items, anthropicMessage{Role: "user", Content: msg.Text})
		default:
			items = append(items, anthropicMessage{Role: string(msg.Role), Content: msg.Text})
		}
	}
	return items
}

func toAnthropicTools(defs []message.ToolDefinition) []anthropicTool {
	items := make([]anthropicTool, 0, len(defs))
	for _, def := range defs {
		items = append(items, anthropicTool{
			Name:        def.Name,
			Description: def.Description,
			InputSchema: def.InputSchema,
		})
	}
	return items
}

// thinkingFromBudget enables Claude extended thinking with the supplied
// budget. The API requires budget_tokens >= 1024, so the provided value is
// floored; a non-positive budget leaves the feature disabled.
func thinkingFromBudget(budget int) *thinkingOptions {
	if budget <= 0 {
		return nil
	}
	if budget < 1024 {
		budget = 1024
	}
	return &thinkingOptions{Type: "enabled", BudgetTokens: budget}
}

func mapAnthropicStopReason(reason string) provider.StopReason {
	switch reason {
	case "end_turn", "stop_sequence":
		return provider.StopReasonComplete
	case "max_tokens":
		return provider.StopReasonMaxTurns
	case "tool_use":
		return provider.StopReasonToolUse
	default:
		return provider.StopReasonUnknown
	}
}
