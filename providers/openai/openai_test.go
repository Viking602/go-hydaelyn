package openai

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

func TestDriverStreamParsesChatCompletionSSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello \"}}]}\n\n"))
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"world\"}}]}\n\n"))
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{\\\"query\\\":\\\"hy\"}}]}}]}\n\n"))
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"daelyn\\\"}\"}}]}}]}\n\n"))
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"index\":0,\"finish_reason\":\"tool_calls\"}]}\n\n"))
		_, _ = writer.Write([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":5,\"total_tokens\":8}}\n\n"))
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	driver := New(Config{
		APIKey:  "test",
		BaseURL: server.URL,
		Client:  server.Client(),
	})
	stream, err := driver.Stream(context.Background(), provider.Request{
		Model: "gpt-test",
		Messages: []message.Message{
			message.NewText(message.RoleUser, "hello"),
		},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	events := collectEvents(t, stream)
	if len(events) < 5 {
		t.Fatalf("expected streamed events, got %#v", events)
	}
	if events[0].Kind != provider.EventTextDelta || events[0].Text != "Hello " {
		t.Fatalf("unexpected first event %#v", events[0])
	}
	if events[2].Kind != provider.EventToolCallDelta || events[2].ToolCallDelta.Name != "lookup" {
		t.Fatalf("expected tool call delta, got %#v", events[2])
	}
	last := events[len(events)-1]
	if last.Kind != provider.EventDone || last.StopReason != provider.StopReasonToolUse {
		t.Fatalf("expected tool-use done event, got %#v", last)
	}
	if last.Usage.TotalTokens != 8 {
		t.Fatalf("expected usage in final event, got %#v", last)
	}
}

func TestDriverStreamExtractsReasoningContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"let me think\"}}]}\n\n"))
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\" harder\"}}]}\n\n"))
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"}}]}\n\n"))
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"index\":0,\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	driver := New(Config{APIKey: "test", BaseURL: server.URL, Client: server.Client()})
	stream, err := driver.Stream(context.Background(), provider.Request{Model: "qwen", Messages: []message.Message{message.NewText(message.RoleUser, "hi")}})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	events := collectEvents(t, stream)
	var thinking, text string
	for _, ev := range events {
		switch ev.Kind {
		case provider.EventThinkingDelta:
			thinking += ev.Thinking
		case provider.EventTextDelta:
			text += ev.Text
		}
	}
	if thinking != "let me think harder" {
		t.Fatalf("thinking = %q, want %q", thinking, "let me think harder")
	}
	if text != "answer" {
		t.Fatalf("text = %q, want %q", text, "answer")
	}
}

func TestDriverStreamExtractsInlineThinkTags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		// Split "<think>" across two chunks to exercise the cross-chunk buffer.
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"pre <thi\"}}]}\n\n"))
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"nk>hidden\"}}]}\n\n"))
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\" thoughts</thi\"}}]}\n\n"))
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"nk> visible\"}}]}\n\n"))
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"index\":0,\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	driver := New(Config{APIKey: "test", BaseURL: server.URL, Client: server.Client()})
	stream, err := driver.Stream(context.Background(), provider.Request{Model: "qwen", Messages: []message.Message{message.NewText(message.RoleUser, "hi")}})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	events := collectEvents(t, stream)
	var thinking, text string
	for _, ev := range events {
		switch ev.Kind {
		case provider.EventThinkingDelta:
			thinking += ev.Thinking
		case provider.EventTextDelta:
			text += ev.Text
		}
	}
	if thinking != "hidden thoughts" {
		t.Fatalf("thinking = %q, want %q", thinking, "hidden thoughts")
	}
	if text != "pre  visible" {
		t.Fatalf("text = %q, want %q", text, "pre  visible")
	}
}

func TestDriverStreamForwardsStopAndReasoning(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewDecoder(request.Body).Decode(&captured)
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	driver := New(Config{APIKey: "test", BaseURL: server.URL, Client: server.Client()})
	stream, err := driver.Stream(context.Background(), provider.Request{
		Model:          "gpt-5.4",
		Messages:       []message.Message{message.NewText(message.RoleUser, "hi")},
		StopSequences:  []string{"Wait,", "Actually,"},
		ThinkingBudget: 5000,
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	_ = collectEvents(t, stream)

	stop, _ := captured["stop"].([]any)
	if len(stop) != 2 || stop[0] != "Wait," {
		t.Fatalf("expected stop sequences forwarded, got %#v", captured["stop"])
	}
	reasoning, _ := captured["reasoning"].(map[string]any)
	if reasoning["effort"] != "medium" {
		t.Fatalf("expected reasoning effort medium for budget=5000, got %#v", reasoning)
	}
}

func collectEvents(t *testing.T, stream provider.Stream) []provider.Event {
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
