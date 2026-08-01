package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/provider/shared"
)

type requestBody struct {
	Model         string             `json:"model"`
	MaxTokens     int                `json:"max_tokens"`
	System        string             `json:"system,omitempty"`
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
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

// contentBlock is the union of the Anthropic content-block shapes the driver
// produces. A single block instance sets only the fields for its Type; all
// other fields are omitted, so one struct safely marshals every variant
// (text / tool_use / tool_result / thinking / redacted_thinking).
type contentBlock struct {
	Type string `json:"type"`
	// text
	Text string `json:"text,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	// thinking
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	// redacted_thinking
	Data string `json:"data,omitempty"`
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
		Data string `json:"data"`
	} `json:"content_block"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		Signature   string `json:"signature"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	// Error carries a mid-stream API error (overload, content policy,
	// invalid request that passed the initial 200). Anthropic's SSE error
	// event shape is {"type":"error","error":{"type":"...","message":"..."}}.
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
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
	system, messages := toAnthropicRequest(request.Messages)
	body, err := json.Marshal(requestBody{
		Model:         request.Model,
		MaxTokens:     maxTokens,
		System:        system,
		Messages:      messages,
		Tools:         toAnthropicTools(request.Tools),
		Stream:        true,
		StopSequences: request.StopSequences,
		Thinking:      thinkingFromBudget(request.ThinkingBudget),
	})
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(d.config.BaseURL, "/") + "/messages"
	client := d.config.Client
	if client == nil {
		client = http.DefaultClient
	}
	idempotencyKey, err := shared.NewIdempotencyKey()
	if err != nil {
		return nil, fmt.Errorf("anthropic: generate idempotency key: %w", err)
	}
	// Stream initiation is retried (never mid-stream): the request body is
	// rebuilt per attempt and transient 429/5xx responses back off per
	// Config.Retry.
	resp, err := shared.DoWithRetry(ctx, client, d.config.Retry, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", d.config.Version)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", idempotencyKey)
		if len(d.config.Betas) > 0 {
			req.Header.Set("anthropic-beta", strings.Join(d.config.Betas, ","))
		}
		return req, nil
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		payload, _ := io.ReadAll(resp.Body)
		return nil, provider.NewHTTPError("anthropic", resp.StatusCode, strings.TrimSpace(string(payload)))
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
			switch parsed.ContentBlock.Type {
			case "tool_use":
				s.state.toolCalls[parsed.Index] = provider.ToolCallDelta{
					ID:   parsed.ContentBlock.ID,
					Name: parsed.ContentBlock.Name,
				}
				current := s.state.toolCalls[parsed.Index]
				s.state.pending = append(s.state.pending, provider.Event{
					Kind:          provider.EventToolCallDelta,
					ToolCallDelta: &current,
				})
			case "redacted_thinking":
				// redacted_thinking arrives whole; carry its opaque payload so
				// the loop can replay it verbatim on a later turn.
				s.state.pending = append(s.state.pending, provider.Event{
					Kind:             provider.EventThinkingDelta,
					RedactedThinking: parsed.ContentBlock.Data,
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
			if parsed.Delta.Type == "signature_delta" {
				// signature_delta arrives just before content_block_stop and
				// carries the thinking block's verifiable signature.
				s.state.pending = append(s.state.pending, provider.Event{
					Kind:      provider.EventThinkingDelta,
					Signature: parsed.Delta.Signature,
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
			return provider.Event{}, anthropicError(parsed.Error.Type, parsed.Error.Message)
		}
	}
}

func anthropicError(errorType, message string) error {
	kind := provider.ErrorUnknown
	switch errorType {
	case "overloaded_error", "api_error":
		kind = provider.ErrorServer
	case "rate_limit_error":
		kind = provider.ErrorRateLimit
	case "invalid_request_error":
		kind = provider.ErrorInvalidRequest
	case "authentication_error":
		kind = provider.ErrorAuthentication
	case "permission_error":
		kind = provider.ErrorPermission
	case "not_found_error":
		kind = provider.ErrorNotFound
	}
	return &provider.Error{Provider: "anthropic", Kind: kind, Code: errorType, Message: message}
}

func (s *anthropicStream) Close() error {
	return s.body.Close()
}

// toAnthropicRequest maps the loop's flat message history onto the Anthropic
// Messages wire format: system messages collapse into the top-level system
// parameter, assistant turns become ordered content-block arrays
// (thinking → redacted_thinking → text → tool_use), and consecutive tool
// results are coalesced into a single user message whose tool_result blocks
// lead it — both requirements of the API.
//
// One thinking block per assistant turn is assumed: the loop accumulates a
// turn's reasoning into a single string, so interleaved multi-block thinking
// is not represented here. A thinking block is only emitted when its
// signature is present, since the API rejects unsigned thinking blocks.
func toAnthropicRequest(messages []message.Message) (string, []anthropicMessage) {
	var systemParts []string
	items := make([]anthropicMessage, 0, len(messages))
	var pendingToolResults []contentBlock

	flush := func() {
		if len(pendingToolResults) > 0 {
			items = append(items, anthropicMessage{Role: "user", Content: pendingToolResults})
			pendingToolResults = nil
		}
	}

	for _, msg := range messages {
		switch msg.Role {
		case message.RoleSystem:
			flush()
			if msg.Text != "" {
				systemParts = append(systemParts, msg.Text)
			}
		case message.RoleTool:
			if msg.ToolResult != nil {
				pendingToolResults = append(pendingToolResults, toolResultBlock(*msg.ToolResult))
			}
		case message.RoleAssistant:
			flush()
			if blocks := assistantBlocks(msg); len(blocks) > 0 {
				items = append(items, anthropicMessage{Role: "assistant", Content: blocks})
			}
		default:
			flush()
			if msg.Text != "" {
				items = append(items, anthropicMessage{
					Role:    "user",
					Content: []contentBlock{{Type: "text", Text: msg.Text}},
				})
			}
		}
	}
	flush()
	return strings.Join(systemParts, "\n\n"), items
}

// assistantBlocks renders an assistant message as an ordered block array.
// Thinking must precede tool_use, and a thinking block requires its signature,
// so an unsigned thinking string is dropped rather than sent (which the API
// would reject).
func assistantBlocks(msg message.Message) []contentBlock {
	blocks := make([]contentBlock, 0, 2+len(msg.ToolCalls))
	if msg.Thinking != "" && msg.ThinkingSignature != "" {
		blocks = append(blocks, contentBlock{
			Type:      "thinking",
			Thinking:  msg.Thinking,
			Signature: msg.ThinkingSignature,
		})
	}
	if msg.RedactedThinking != "" {
		blocks = append(blocks, contentBlock{Type: "redacted_thinking", Data: msg.RedactedThinking})
	}
	if msg.Text != "" {
		blocks = append(blocks, contentBlock{Type: "text", Text: msg.Text})
	}
	for _, call := range msg.ToolCalls {
		input := json.RawMessage(call.Arguments)
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		blocks = append(blocks, contentBlock{
			Type:  "tool_use",
			ID:    call.ID,
			Name:  call.Name,
			Input: input,
		})
	}
	return blocks
}

func toolResultBlock(result message.ToolResult) contentBlock {
	return contentBlock{
		Type:      "tool_result",
		ToolUseID: result.ToolCallID,
		Content:   result.Content,
		IsError:   result.IsError,
	}
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
