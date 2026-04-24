package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/provider/shared"
)

type chatCompletionRequest struct {
	Model          string            `json:"model"`
	Messages       []chatMessage     `json:"messages"`
	Tools          []chatTool        `json:"tools,omitempty"`
	Stream         bool              `json:"stream"`
	StreamOptions  streamOptions     `json:"stream_options,omitempty"`
	Stop           []string          `json:"stop,omitempty"`
	Reasoning      *reasoningOptions `json:"reasoning,omitempty"`
	ResponseFormat any               `json:"response_format,omitempty"`
}

type reasoningOptions struct {
	Effort string `json:"effort,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type chatTool struct {
	Type     string           `json:"type"`
	Function chatToolFunction `json:"function"`
}

type chatToolFunction struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Parameters  message.JSONSchema `json:"parameters"`
}

type chatToolCall struct {
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type,omitempty"`
	Function chatToolCallDetail `json:"function"`
}

type chatToolCallDetail struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type chunk struct {
	Choices []choiceChunk `json:"choices"`
	Usage   struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type choiceChunk struct {
	Delta struct {
		Content          string              `json:"content"`
		ReasoningContent string              `json:"reasoning_content"`
		Reasoning        string              `json:"reasoning"`
		ToolCalls        []toolCallDeltaItem `json:"tool_calls"`
	} `json:"delta"`
	FinishReason string `json:"finish_reason"`
}

type toolCallDeltaItem struct {
	Index    *int   `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type streamState struct {
	reader     *shared.Reader
	pending    []provider.Event
	finished   bool
	usage      provider.Usage
	stopReason provider.StopReason
	splitter   thinkSplitter
}

// thinkSplitter extracts <think>...</think> segments from a streamed token
// sequence. Tags may be split across chunks, so it buffers the trailing bytes
// that could still be a tag prefix until enough input arrives to decide.
type thinkSplitter struct {
	inThink bool
	buffer  string
}

const (
	thinkOpen  = "<think>"
	thinkClose = "</think>"
)

func (t *thinkSplitter) process(delta string) (text string, thinking string) {
	t.buffer += delta
	var textB, thinkB strings.Builder
	for {
		if t.inThink {
			idx := strings.Index(t.buffer, thinkClose)
			if idx >= 0 {
				thinkB.WriteString(t.buffer[:idx])
				t.buffer = t.buffer[idx+len(thinkClose):]
				t.inThink = false
				continue
			}
			safe := safeEmitLen(t.buffer, thinkClose)
			thinkB.WriteString(t.buffer[:safe])
			t.buffer = t.buffer[safe:]
			break
		}
		idx := strings.Index(t.buffer, thinkOpen)
		if idx >= 0 {
			textB.WriteString(t.buffer[:idx])
			t.buffer = t.buffer[idx+len(thinkOpen):]
			t.inThink = true
			continue
		}
		safe := safeEmitLen(t.buffer, thinkOpen)
		textB.WriteString(t.buffer[:safe])
		t.buffer = t.buffer[safe:]
		break
	}
	return textB.String(), thinkB.String()
}

// flush drains any bytes still buffered at stream end. Residual inside a
// <think> block is emitted as thinking; otherwise as text.
func (t *thinkSplitter) flush() (text string, thinking string) {
	if t.buffer == "" {
		return "", ""
	}
	out := t.buffer
	t.buffer = ""
	if t.inThink {
		return "", out
	}
	return out, ""
}

// safeEmitLen returns the count of leading bytes of s that cannot be the
// start of target. The suffix that is withheld may complete into target on
// the next chunk.
func safeEmitLen(s, target string) int {
	max := len(target) - 1
	if max > len(s) {
		max = len(s)
	}
	for k := max; k >= 1; k-- {
		if strings.HasPrefix(target, s[len(s)-k:]) {
			return len(s) - k
		}
	}
	return len(s)
}

func (d Driver) Stream(ctx context.Context, request provider.Request) (provider.Stream, error) {
	apiKey := d.config.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("openai api key is required")
	}
	body, err := marshalChatCompletionRequest(chatCompletionRequest{
		Model:          request.Model,
		Messages:       toChatMessages(request.Messages),
		Tools:          toChatTools(request.Tools),
		Stream:         true,
		StreamOptions:  streamOptions{IncludeUsage: true},
		Stop:           request.StopSequences,
		Reasoning:      reasoningFromBudget(request.ThinkingBudget),
		ResponseFormat: responseFormatFromRequest(request.ResponseFormat),
	}, request.ExtraBody)
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(d.config.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := d.config.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
		return nil, fmt.Errorf("openai api returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	if !isEventStreamContentType(resp.Header.Get("Content-Type")) {
		defer resp.Body.Close()
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
		return nil, fmt.Errorf("openai api returned unexpected content type %q: %s", resp.Header.Get("Content-Type"), strings.TrimSpace(string(payload)))
	}
	return &openAIStream{
		body:  resp.Body,
		state: streamState{reader: shared.NewReader(resp.Body)},
	}, nil
}

func marshalChatCompletionRequest(payload chatCompletionRequest, extraBody map[string]any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return marshalChatCompletionRequestBody(body, extraChatCompletionBodyFields(extraBody))
}

func marshalChatCompletionRequestBody(body []byte, extraFields map[string]any) ([]byte, error) {
	merged := map[string]any{}
	if err := json.Unmarshal(body, &merged); err != nil {
		return nil, err
	}
	maps.Copy(merged, extraFields)
	return json.Marshal(merged)
}

func extraChatCompletionBodyFields(extraBody map[string]any) map[string]any {
	fields := make(map[string]any, len(extraBody))
	maps.Copy(fields, extraBody)
	for key := range managedChatCompletionBodyFields {
		delete(fields, key)
	}
	return fields
}

var managedChatCompletionBodyFields = map[string]struct{}{
	"model":           {},
	"messages":        {},
	"tools":           {},
	"stream":          {},
	"stream_options":  {},
	"stop":            {},
	"reasoning":       {},
	"response_format": {},
}

func responseFormatFromRequest(format *provider.ResponseFormat) any {
	if format == nil || format.Type == "" {
		return nil
	}
	switch format.Type {
	case "json_object":
		return map[string]any{"type": "json_object"}
	case "json_schema":
		payload := map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   format.Name,
				"strict": format.Strict,
			},
		}
		if format.Schema != nil {
			payload["json_schema"].(map[string]any)["schema"] = format.Schema
		}
		return payload
	default:
		return nil
	}
}

type openAIStream struct {
	body  io.ReadCloser
	state streamState
}

func (s *openAIStream) Recv() (provider.Event, error) {
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
		if current.Data == "[DONE]" {
			s.state.finished = true
			if text, thinking := s.state.splitter.flush(); text != "" || thinking != "" {
				if thinking != "" {
					s.state.pending = append(s.state.pending, provider.Event{
						Kind:     provider.EventThinkingDelta,
						Thinking: thinking,
					})
				}
				if text != "" {
					s.state.pending = append(s.state.pending, provider.Event{
						Kind: provider.EventTextDelta,
						Text: text,
					})
				}
			}
			s.state.pending = append(s.state.pending, provider.Event{
				Kind:       provider.EventDone,
				Usage:      s.state.usage,
				StopReason: s.state.stopReason,
			})
			continue
		}
		var parsed chunk
		if err := json.Unmarshal([]byte(current.Data), &parsed); err != nil {
			return provider.Event{}, err
		}
		if parsed.Usage.TotalTokens > 0 {
			s.state.usage = provider.Usage{
				InputTokens:  parsed.Usage.PromptTokens,
				OutputTokens: parsed.Usage.CompletionTokens,
				TotalTokens:  parsed.Usage.TotalTokens,
			}
		}
		for _, choice := range parsed.Choices {
			if reasoning := choice.Delta.ReasoningContent; reasoning != "" {
				s.state.pending = append(s.state.pending, provider.Event{
					Kind:     provider.EventThinkingDelta,
					Thinking: reasoning,
				})
			} else if reasoning := choice.Delta.Reasoning; reasoning != "" {
				s.state.pending = append(s.state.pending, provider.Event{
					Kind:     provider.EventThinkingDelta,
					Thinking: reasoning,
				})
			}
			if choice.Delta.Content != "" {
				text, thinking := s.state.splitter.process(choice.Delta.Content)
				if thinking != "" {
					s.state.pending = append(s.state.pending, provider.Event{
						Kind:     provider.EventThinkingDelta,
						Thinking: thinking,
					})
				}
				if text != "" {
					s.state.pending = append(s.state.pending, provider.Event{
						Kind: provider.EventTextDelta,
						Text: text,
					})
				}
			}
			for _, item := range choice.Delta.ToolCalls {
				s.state.pending = append(s.state.pending, provider.Event{
					Kind: provider.EventToolCallDelta,
					ToolCallDelta: &provider.ToolCallDelta{
						Index:          item.Index,
						ID:             item.ID,
						Name:           item.Function.Name,
						ArgumentsDelta: item.Function.Arguments,
					},
				})
			}
			if choice.FinishReason != "" {
				s.state.stopReason = mapOpenAIStopReason(choice.FinishReason)
			}
		}
	}
}

func (s *openAIStream) Close() error {
	return s.body.Close()
}

func toChatMessages(messages []message.Message) []chatMessage {
	items := make([]chatMessage, 0, len(messages))
	for _, msg := range messages {
		item := chatMessage{Role: string(msg.Role)}
		switch msg.Role {
		case message.RoleAssistant:
			item.Content = msg.Text
			if len(msg.ToolCalls) > 0 {
				item.ToolCalls = make([]chatToolCall, 0, len(msg.ToolCalls))
				for _, call := range msg.ToolCalls {
					item.ToolCalls = append(item.ToolCalls, chatToolCall{
						ID:   call.ID,
						Type: "function",
						Function: chatToolCallDetail{
							Name:      call.Name,
							Arguments: string(call.Arguments),
						},
					})
				}
			}
		case message.RoleTool:
			if msg.ToolResult != nil {
				item.Content = msg.ToolResult.Content
				item.ToolCallID = msg.ToolResult.ToolCallID
			}
		default:
			item.Content = msg.Text
		}
		items = append(items, item)
	}
	return items
}

func toChatTools(defs []message.ToolDefinition) []chatTool {
	items := make([]chatTool, 0, len(defs))
	for _, def := range defs {
		items = append(items, chatTool{
			Type: "function",
			Function: chatToolFunction{
				Name:        def.Name,
				Description: def.Description,
				Parameters:  def.InputSchema,
			},
		})
	}
	return items
}

// reasoningFromBudget maps a token-style budget hint onto the OpenAI
// reasoning.effort enum used by GPT-5 and o-series models. Returning nil
// means the caller opted out and the field is omitted from the request.
func reasoningFromBudget(budget int) *reasoningOptions {
	if budget <= 0 {
		return nil
	}
	switch {
	case budget < 2000:
		return &reasoningOptions{Effort: "low"}
	case budget < 10000:
		return &reasoningOptions{Effort: "medium"}
	default:
		return &reasoningOptions{Effort: "high"}
	}
}

func mapOpenAIStopReason(reason string) provider.StopReason {
	switch reason {
	case "stop":
		return provider.StopReasonComplete
	case "length":
		return provider.StopReasonMaxTurns
	case "tool_calls", "function_call":
		return provider.StopReasonToolUse
	case "content_filter":
		return provider.StopReasonError
	default:
		return provider.StopReasonUnknown
	}
}

func isEventStreamContentType(contentType string) bool {
	if strings.TrimSpace(contentType) == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}
	return strings.EqualFold(mediaType, "text/event-stream")
}
