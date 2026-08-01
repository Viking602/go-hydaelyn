package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/provider/shared"
)

func TestNewDefaultClientHasNoStreamLifetimeTimeout(t *testing.T) {
	driver := New(Config{})
	if driver.config.Client == nil {
		t.Fatal("expected default client")
	}
	if driver.config.Client.Timeout != 0 {
		t.Fatalf("default client timeout = %s, want 0", driver.config.Client.Timeout)
	}
	transport, ok := driver.config.Client.Transport.(*http.Transport)
	if !ok || transport.ResponseHeaderTimeout <= 0 {
		t.Fatalf("default response header timeout is not configured")
	}

	supplied := &http.Client{}
	driver = New(Config{Client: supplied})
	if driver.config.Client != supplied {
		t.Fatal("expected supplied client to be preserved")
	}
}

func TestDriverStreamParsesMessageSSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/messages" {
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello \"}}\n\n"))
		_, _ = writer.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"lookup\",\"input\":{}}}\n\n"))
		_, _ = writer.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"query\\\":\\\"ve\"}}\n\n"))
		_, _ = writer.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"nat\\\"}\"}}\n\n"))
		_, _ = writer.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":15}}\n\n"))
		_, _ = writer.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	driver := New(Config{
		APIKey:  "test",
		BaseURL: server.URL,
		Client:  server.Client(),
	})
	stream, err := driver.Stream(context.Background(), provider.Request{
		Model: "claude-test",
		Messages: []message.Message{
			message.NewText(message.RoleUser, "hello"),
		},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	events := collectAnthropicEvents(t, stream)
	if len(events) < 4 {
		t.Fatalf("expected streamed events, got %#v", events)
	}
	if events[0].Kind != provider.EventTextDelta || events[0].Text != "Hello " {
		t.Fatalf("unexpected first event %#v", events[0])
	}
	if events[1].Kind != provider.EventToolCallDelta || events[1].ToolCallDelta.Name != "lookup" {
		t.Fatalf("expected tool call start delta, got %#v", events[1])
	}
	last := events[len(events)-1]
	if last.Kind != provider.EventDone || last.StopReason != provider.StopReasonToolUse {
		t.Fatalf("expected tool-use done event, got %#v", last)
	}
	if last.Usage.OutputTokens != 15 {
		t.Fatalf("expected usage in final event, got %#v", last)
	}
}

func TestDriverStreamForwardsStopAndThinking(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewDecoder(request.Body).Decode(&captured)
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"reasoning...\"}}\n\n"))
		_, _ = writer.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"answer\"}}\n\n"))
		_, _ = writer.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n"))
		_, _ = writer.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	driver := New(Config{APIKey: "test", BaseURL: server.URL, Client: server.Client()})
	stream, err := driver.Stream(context.Background(), provider.Request{
		Model:          "claude-test",
		Messages:       []message.Message{message.NewText(message.RoleUser, "hi")},
		StopSequences:  []string{"Wait,"},
		ThinkingBudget: 500, // below 1024; driver should floor to 1024
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	events := collectAnthropicEvents(t, stream)

	stop, _ := captured["stop_sequences"].([]any)
	if len(stop) != 1 || stop[0] != "Wait," {
		t.Fatalf("expected stop_sequences forwarded, got %#v", captured["stop_sequences"])
	}
	thinking, _ := captured["thinking"].(map[string]any)
	if thinking["type"] != "enabled" {
		t.Fatalf("expected thinking enabled, got %#v", thinking)
	}
	if int(thinking["budget_tokens"].(float64)) != 1024 {
		t.Fatalf("expected budget_tokens floored to 1024, got %#v", thinking["budget_tokens"])
	}

	var sawThinking bool
	for _, ev := range events {
		if ev.Kind == provider.EventThinkingDelta && ev.Thinking == "reasoning..." {
			sawThinking = true
		}
	}
	if !sawThinking {
		t.Fatalf("expected EventThinkingDelta from thinking_delta, events=%#v", events)
	}
}

func TestToAnthropicRequestThinkingToolRoundTrip(t *testing.T) {
	history := []message.Message{
		{Role: message.RoleSystem, Text: "you are helpful"},
		message.NewText(message.RoleUser, "weather?"),
		{
			Role:              message.RoleAssistant,
			Thinking:          "let me check",
			ThinkingSignature: "sig-abc",
			ToolCalls: []message.ToolCall{
				{ID: "toolu_1", Name: "weather", Arguments: []byte(`{"city":"SF"}`)},
			},
		},
		message.NewToolResult(message.ToolResult{ToolCallID: "toolu_1", Name: "weather", Content: "sunny"}),
	}

	system, messages := toAnthropicRequest(history)
	if system != "you are helpful" {
		t.Fatalf("system = %q, want extracted system text", system)
	}
	if len(messages) != 3 {
		t.Fatalf("expected user/assistant/tool-result messages, got %d: %#v", len(messages), messages)
	}
	assistant := messages[1]
	if assistant.Role != "assistant" || len(assistant.Content) != 2 {
		t.Fatalf("unexpected assistant message %#v", assistant)
	}
	if assistant.Content[0].Type != "thinking" || assistant.Content[0].Signature != "sig-abc" {
		t.Fatalf("expected leading signed thinking block, got %#v", assistant.Content[0])
	}
	if assistant.Content[1].Type != "tool_use" || assistant.Content[1].ID != "toolu_1" {
		t.Fatalf("expected tool_use block, got %#v", assistant.Content[1])
	}
	toolMsg := messages[2]
	if toolMsg.Role != "user" || len(toolMsg.Content) != 1 {
		t.Fatalf("unexpected tool-result message %#v", toolMsg)
	}
	block := toolMsg.Content[0]
	if block.Type != "tool_result" || block.ToolUseID != "toolu_1" || block.Content != "sunny" {
		t.Fatalf("expected tool_result carrying tool_use_id, got %#v", block)
	}
}

func TestToAnthropicRequestCoalescesToolResults(t *testing.T) {
	history := []message.Message{
		{
			Role: message.RoleAssistant,
			ToolCalls: []message.ToolCall{
				{ID: "a", Name: "t", Arguments: []byte(`{}`)},
				{ID: "b", Name: "t", Arguments: []byte(`{}`)},
			},
		},
		message.NewToolResult(message.ToolResult{ToolCallID: "a", Name: "t", Content: "one"}),
		message.NewToolResult(message.ToolResult{ToolCallID: "b", Name: "t", Content: "two", IsError: true}),
	}

	_, messages := toAnthropicRequest(history)
	if len(messages) != 2 {
		t.Fatalf("expected assistant + single coalesced user message, got %d: %#v", len(messages), messages)
	}
	user := messages[1]
	if user.Role != "user" || len(user.Content) != 2 {
		t.Fatalf("expected two tool_result blocks in one user message, got %#v", user)
	}
	if user.Content[0].ToolUseID != "a" || user.Content[1].ToolUseID != "b" {
		t.Fatalf("tool_use_id ordering wrong: %#v", user.Content)
	}
	if !user.Content[1].IsError {
		t.Fatalf("expected is_error on second tool result, got %#v", user.Content[1])
	}
}

func TestToAnthropicRequestDropsUnsignedThinking(t *testing.T) {
	history := []message.Message{
		{Role: message.RoleAssistant, Thinking: "no signature here", Text: "answer"},
	}

	_, messages := toAnthropicRequest(history)
	if len(messages) != 1 {
		t.Fatalf("expected one assistant message, got %#v", messages)
	}
	for _, block := range messages[0].Content {
		if block.Type == "thinking" {
			t.Fatalf("unsigned thinking block should be dropped, got %#v", messages[0].Content)
		}
	}
	if len(messages[0].Content) != 1 || messages[0].Content[0].Type != "text" {
		t.Fatalf("expected only the text block, got %#v", messages[0].Content)
	}
}

func TestToAnthropicRequestEmptyToolInput(t *testing.T) {
	history := []message.Message{
		{Role: message.RoleAssistant, ToolCalls: []message.ToolCall{{ID: "x", Name: "noop"}}},
	}

	_, messages := toAnthropicRequest(history)
	block := messages[0].Content[0]
	if block.Type != "tool_use" || string(block.Input) != "{}" {
		t.Fatalf("expected empty input rendered as {}, got %#v", block)
	}
	raw, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"input":{}`) {
		t.Fatalf("expected input key present in %s", raw)
	}
}

func TestDriverStreamSendsSystemAndBlocks(t *testing.T) {
	var captured requestBody
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewDecoder(request.Body).Decode(&captured)
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	driver := New(Config{APIKey: "test", BaseURL: server.URL, Client: server.Client()})
	stream, err := driver.Stream(context.Background(), provider.Request{
		Model: "claude-test",
		Messages: []message.Message{
			{Role: message.RoleSystem, Text: "be terse"},
			message.NewText(message.RoleUser, "hi"),
		},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	_ = collectAnthropicEvents(t, stream)

	if captured.System != "be terse" {
		t.Fatalf("system = %q, want top-level system param", captured.System)
	}
	if len(captured.Messages) != 1 || captured.Messages[0].Role != "user" {
		t.Fatalf("expected single user message, got %#v", captured.Messages)
	}
	content := captured.Messages[0].Content
	if len(content) != 1 || content[0].Type != "text" || content[0].Text != "hi" {
		t.Fatalf("expected text block content, got %#v", content)
	}
}

func TestDriverStreamCapturesThinkingSignature(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"reasoning\"}}\n\n"))
		_, _ = writer.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"sig-xyz\"}}\n\n"))
		_, _ = writer.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n"))
		_, _ = writer.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	driver := New(Config{APIKey: "test", BaseURL: server.URL, Client: server.Client()})
	stream, err := driver.Stream(context.Background(), provider.Request{
		Model:    "claude-test",
		Messages: []message.Message{message.NewText(message.RoleUser, "hi")},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	normalized, err := provider.NormalizeEvents(collectAnthropicEvents(t, stream))
	if err != nil {
		t.Fatalf("NormalizeEvents() error = %v", err)
	}
	if normalized.Thinking != "reasoning" {
		t.Fatalf("thinking = %q, want accumulated reasoning", normalized.Thinking)
	}
	if normalized.Signature != "sig-xyz" {
		t.Fatalf("signature = %q, want captured signature", normalized.Signature)
	}
}

func TestDriverStreamCapturesRedactedThinking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"redacted_thinking\",\"data\":\"enc-123\"}}\n\n"))
		_, _ = writer.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n"))
		_, _ = writer.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	driver := New(Config{APIKey: "test", BaseURL: server.URL, Client: server.Client()})
	stream, err := driver.Stream(context.Background(), provider.Request{
		Model:    "claude-test",
		Messages: []message.Message{message.NewText(message.RoleUser, "hi")},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	normalized, err := provider.NormalizeEvents(collectAnthropicEvents(t, stream))
	if err != nil {
		t.Fatalf("NormalizeEvents() error = %v", err)
	}
	if normalized.RedactedThinking != "enc-123" {
		t.Fatalf("redacted thinking = %q, want captured payload", normalized.RedactedThinking)
	}
}

func collectAnthropicEvents(t *testing.T, stream provider.Stream) []provider.Event {
	t.Helper()
	defer func() { _ = stream.Close() }()
	events := make([]provider.Event, 0, 8)
	for {
		event, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("Recv() error = %v", err)
		}
		events = append(events, event)
	}
	return events
}

func TestStreamRetriesTransientStatusOnInitiation(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		if calls < 3 {
			writer.WriteHeader(http.StatusTooManyRequests)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"))
		_, _ = writer.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	driver := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Retry:   shared.RetryPolicy{BaseDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond},
	})
	stream, err := driver.Stream(context.Background(), provider.Request{
		Model:    "claude-sonnet-4-6",
		Messages: []message.Message{message.NewText(message.RoleUser, "hi")},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v, want success after two 429 retries", err)
	}
	defer func() { _ = stream.Close() }()
	event, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv() error = %v", err)
	}
	if event.Text != "ok" {
		t.Fatalf("event text = %q, want ok", event.Text)
	}
	if calls != 3 {
		t.Fatalf("server saw %d calls, want 3", calls)
	}
}

// TestDriverStreamSurfacesTypedError is the regression for opaque mid-stream
// overload errors: adapters must preserve details and map the wire error to the
// provider-neutral retry contract.
func TestDriverStreamSurfacesTypedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n"))
	}))
	defer server.Close()

	driver := New(Config{APIKey: "test", BaseURL: server.URL, Client: server.Client()})
	stream, err := driver.Stream(context.Background(), provider.Request{
		Model:    "claude-test",
		Messages: []message.Message{message.NewText(message.RoleUser, "hi")},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	defer func() { _ = stream.Close() }()

	_, err = stream.Recv()
	if err == nil {
		t.Fatal("Recv() expected error for mid-stream error event, got nil")
	}
	if !strings.Contains(err.Error(), "overloaded_error") {
		t.Fatalf("error = %q, want it to contain the upstream type %q", err.Error(), "overloaded_error")
	}
	if !strings.Contains(err.Error(), "Overloaded") {
		t.Fatalf("error = %q, want it to contain the upstream message %q", err.Error(), "Overloaded")
	}
	if provider.ErrorKindOf(err) != provider.ErrorServer || !provider.IsRetryableError(err) {
		t.Fatalf("error classification = %q retryable=%v", provider.ErrorKindOf(err), provider.IsRetryableError(err))
	}
}
