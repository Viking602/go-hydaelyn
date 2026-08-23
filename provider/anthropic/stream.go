package anthropic

import (
	"bytes"
	"context"
	"encoding/base64"
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
	Temperature   float64            `json:"temperature,omitempty"`
	TopP          float64            `json:"top_p,omitempty"`
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
	// image / document
	Source *anthropicContentSource `json:"source,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   any    `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	// thinking
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	// redacted_thinking
	Data string `json:"data,omitempty"`
}

type anthropicContentSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

type anthropicTool struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	InputSchema message.JSONSchema `json:"input_schema"`
}

type eventEnvelope struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message struct {
		ID    string `json:"id"`
		Model string `json:"model"`
	} `json:"message"`
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
		InputTokens              int  `json:"input_tokens"`
		CacheReadInputTokens     *int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens *int `json:"cache_creation_input_tokens"`
		OutputTokens             int  `json:"output_tokens"`
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
	response   provider.ResponseMetadata
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
	if request.MaxTokens > 0 {
		maxTokens = request.MaxTokens
	}
	system, messages, err := toAnthropicRequest(request.Messages)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(requestBody{
		Model:         request.Model,
		MaxTokens:     maxTokens,
		Temperature:   request.Temperature,
		TopP:          request.TopP,
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
	client := shared.ClientOrDefault(d.config.Client, defaultResponseHeaderTimeout)
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
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer func() { _ = resp.Body.Close() }()
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
		return nil, provider.NewHTTPError("anthropic", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	if !shared.IsEventStreamContentType(resp.Header.Get("Content-Type")) {
		defer func() { _ = resp.Body.Close() }()
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
		return nil, fmt.Errorf("anthropic api returned unexpected content type %q: %s", resp.Header.Get("Content-Type"), strings.TrimSpace(string(payload)))
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
		if strings.TrimSpace(current.Data) == "" {
			continue
		}
		var parsed eventEnvelope
		if err := json.Unmarshal([]byte(current.Data), &parsed); err != nil {
			return provider.Event{}, err
		}
		switch parsed.Type {
		case "message_start":
			cacheReadTokens, cacheReadReported := optionalToken(parsed.Usage.CacheReadInputTokens)
			cacheWriteTokens, cacheWriteReported := optionalToken(parsed.Usage.CacheCreationInputTokens)
			s.state.usage.InputTokens = parsed.Usage.InputTokens + cacheReadTokens + cacheWriteTokens
			s.state.usage.CachedInputTokens = cacheReadTokens
			s.state.usage.CachedInputTokensReported = cacheReadReported
			s.state.usage.CacheWriteInputTokens = cacheWriteTokens
			s.state.usage.CacheWriteInputTokensReported = cacheWriteReported
			s.state.response = provider.ResponseMetadata{ID: parsed.Message.ID, Model: parsed.Message.Model}
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
					Kind:      provider.EventTextDelta,
					Text:      parsed.Delta.Text,
					TextPhase: provider.TextPhaseFinalAnswer,
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
				Response:   s.state.response,
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
func toAnthropicRequest(messages []message.Message) (string, []anthropicMessage, error) {
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
			text, err := anthropicSystemText(msg.CanonicalContent())
			if err != nil {
				return "", nil, err
			}
			if text != "" {
				systemParts = append(systemParts, text)
			}
		case message.RoleTool:
			if msg.ToolResult != nil {
				block, err := toolResultBlock(*msg.ToolResult)
				if err != nil {
					return "", nil, err
				}
				pendingToolResults = append(pendingToolResults, block)
			}
		case message.RoleAssistant:
			flush()
			blocks, err := assistantBlocks(msg)
			if err != nil {
				return "", nil, err
			}
			if len(blocks) > 0 {
				items = append(items, anthropicMessage{Role: "assistant", Content: blocks})
			}
		default:
			flush()
			blocks, err := anthropicInputBlocks(msg.CanonicalContent(), msg.Role)
			if err != nil {
				return "", nil, err
			}
			if len(blocks) > 0 {
				items = append(items, anthropicMessage{Role: "user", Content: blocks})
			}
		}
	}
	flush()
	return strings.Join(systemParts, "\n\n"), items, nil
}

func anthropicSystemText(parts []message.ContentPart) (string, error) {
	var text strings.Builder
	for _, part := range parts {
		switch part.Kind {
		case message.ContentText, message.ContentCommentary, message.ContentFinalAnswer:
			text.WriteString(part.Text)
		default:
			return "", fmt.Errorf("anthropic system message cannot serialize %s content", part.Kind)
		}
	}
	return text.String(), nil
}

func assistantBlocks(msg message.Message) ([]contentBlock, error) {
	parts := msg.CanonicalContent()
	blocks := make([]contentBlock, 0, len(parts)+len(msg.ToolCalls))
	sawVisible := false
	for _, part := range parts {
		switch part.Kind {
		case message.ContentReasoning:
			if part.Signature == "" {
				continue
			}
			if sawVisible {
				return nil, fmt.Errorf("anthropic signed thinking must precede visible assistant content")
			}
			blocks = append(blocks, contentBlock{Type: "thinking", Thinking: part.Text, Signature: part.Signature})
		case message.ContentRedactedReasoning:
			if sawVisible {
				return nil, fmt.Errorf("anthropic redacted thinking must precede visible assistant content")
			}
			blocks = append(blocks, contentBlock{Type: "redacted_thinking", Data: string(part.Data)})
		case message.ContentText, message.ContentCommentary, message.ContentFinalAnswer:
			sawVisible = true
			blocks = append(blocks, contentBlock{Type: "text", Text: part.Text})
		default:
			return nil, fmt.Errorf("anthropic assistant message cannot serialize %s content", part.Kind)
		}
	}
	for _, call := range msg.ToolCalls {
		input := json.RawMessage(call.Arguments)
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		blocks = append(blocks, contentBlock{Type: "tool_use", ID: call.ID, Name: call.Name, Input: input})
	}
	return blocks, nil
}

func anthropicInputBlocks(parts []message.ContentPart, role message.Role) ([]contentBlock, error) {
	blocks := make([]contentBlock, 0, len(parts))
	for _, part := range parts {
		switch part.Kind {
		case message.ContentText, message.ContentCommentary, message.ContentFinalAnswer:
			blocks = append(blocks, contentBlock{Type: "text", Text: part.Text})
		case message.ContentImage:
			source, err := anthropicSource(part)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, contentBlock{Type: "image", Source: source})
		case message.ContentFile:
			source, err := anthropicSource(part)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, contentBlock{Type: "document", Source: source})
		default:
			return nil, fmt.Errorf("anthropic %s message cannot serialize %s content", role, part.Kind)
		}
	}
	return blocks, nil
}

func anthropicSource(part message.ContentPart) (*anthropicContentSource, error) {
	if part.URI != "" {
		return &anthropicContentSource{Type: "url", URL: part.URI}, nil
	}
	if len(part.Data) == 0 || part.MediaType == "" {
		return nil, fmt.Errorf("anthropic %s content requires uri or inline data with media type", part.Kind)
	}
	return &anthropicContentSource{
		Type:      "base64",
		MediaType: part.MediaType,
		Data:      base64.StdEncoding.EncodeToString(part.Data),
	}, nil
}

func toolResultBlock(result message.ToolResult) (contentBlock, error) {
	parts := result.CanonicalContent()
	blocks, err := anthropicInputBlocks(parts, message.RoleTool)
	if err != nil {
		return contentBlock{}, err
	}
	var content any = blocks
	if len(blocks) == 1 && blocks[0].Type == "text" {
		content = blocks[0].Text
	}
	return contentBlock{
		Type:      "tool_result",
		ToolUseID: result.ToolCallID,
		Content:   content,
		IsError:   result.IsError,
	}, nil
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
		return provider.StopReasonLength
	case "tool_use":
		return provider.StopReasonToolUse
	default:
		return provider.StopReasonUnknown
	}
}

func optionalToken(value *int) (int, bool) {
	if value == nil {
		return 0, false
	}
	return max(0, *value), true
}
