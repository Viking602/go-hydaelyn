package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
)

func TestDriverStreamParsesMessageSSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/messages" {
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello \"}}\n\n"))
		_, _ = writer.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"lookup\",\"input\":{}}}\n\n"))
		_, _ = writer.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"query\\\":\\\"hy\"}}\n\n"))
		_, _ = writer.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"daelyn\\\"}\"}}\n\n"))
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

func collectAnthropicEvents(t *testing.T, stream provider.Stream) []provider.Event {
	t.Helper()
	defer stream.Close()
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
