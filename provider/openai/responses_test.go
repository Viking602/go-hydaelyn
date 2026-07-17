package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/provider/shared"
	"github.com/Viking602/go-hydaelyn/tool"
)

func TestNewDefaultsToChatCompletionsWire(t *testing.T) {
	driver := New(Config{})
	if driver.config.WireAPI != WireChatCompletions {
		t.Fatalf("WireAPI = %q, want %q", driver.config.WireAPI, WireChatCompletions)
	}
}

func TestDriverStreamRejectsUnknownWireAPI(t *testing.T) {
	driver := New(Config{WireAPI: WireAPI("unknown")})
	stream, err := driver.Stream(context.Background(), provider.Request{})
	if stream != nil {
		_ = stream.Close()
		t.Fatal("Stream() returned a stream for an unknown wire API")
	}
	if err == nil || !strings.Contains(err.Error(), `unsupported wire API "unknown"`) {
		t.Fatalf("Stream() error = %v, want unsupported wire API", err)
	}
}

func TestDriverStreamBuildsResponsesRequest(t *testing.T) {
	var (
		captured      map[string]any
		capturedPath  string
		authorization string
	)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		capturedPath = request.URL.Path
		authorization = request.Header.Get("Authorization")
		if err := json.NewDecoder(request.Body).Decode(&captured); err != nil {
			t.Errorf("decode request: %v", err)
		}
		writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		_, _ = writer.Write([]byte("event: response.completed\n"))
		_, _ = writer.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"output\":[],\"usage\":{\"input_tokens\":4,\"output_tokens\":2,\"total_tokens\":6}}}\n\n"))
	}))
	defer server.Close()

	driver := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Client:  server.Client(),
		WireAPI: WireResponses,
	})
	stream, err := driver.Stream(context.Background(), provider.Request{
		Model: "gpt-5-codex",
		Messages: []message.Message{
			message.NewText(message.RoleSystem, "follow instructions"),
			message.NewText(message.RoleUser, "look it up"),
			{
				Role: message.RoleAssistant,
				Text: "I will check.",
				ToolCalls: []message.ToolCall{{
					ID:        "call_1",
					Name:      "lookup",
					Arguments: json.RawMessage(`{"query":"hydaelyn"}`),
				}},
			},
			message.NewToolResult(message.ToolResult{
				ToolCallID: "call_1",
				Name:       "lookup",
				Content:    "found",
			}),
		},
		Tools: []message.ToolDefinition{{
			Name:        "lookup",
			Description: "Look up a project",
			InputSchema: message.JSONSchema{
				Type: "object",
				Properties: map[string]message.JSONSchema{
					"query": {Type: "string"},
				},
				Required: []string{"query"},
			},
		}},
		ThinkingBudget: 5000,
		ResponseFormat: &provider.ResponseFormat{
			Type:   "json_schema",
			Name:   "report",
			Strict: true,
			Schema: &message.JSONSchema{Type: "object"},
		},
		ExtraBody: map[string]any{
			"include": []string{
				responsesEncryptedReasoningInclude,
				"message.output_text.logprobs",
				"message.output_text.logprobs",
			},
			"model":       "overridden",
			"input":       "overridden",
			"tools":       "overridden",
			"stream":      false,
			"reasoning":   map[string]any{"effort": "low"},
			"text":        map[string]any{"format": map[string]any{"type": "text"}},
			"temperature": 0.2,
		},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	events := collectEvents(t, stream)
	if len(events) != 1 || events[0].Kind != provider.EventDone {
		t.Fatalf("events = %#v, want one done event", events)
	}
	if capturedPath != "/responses" {
		t.Fatalf("request path = %q, want /responses", capturedPath)
	}
	if authorization != "Bearer test-key" {
		t.Fatalf("Authorization = %q, want bearer token", authorization)
	}
	requireCapturedField(t, captured, "model", "gpt-5-codex")
	requireCapturedField(t, captured, "stream", true)
	requireCapturedField(t, captured, "temperature", 0.2)
	requireResponsesInput(t, captured["input"])
	requireResponsesTools(t, captured["tools"])
	requireResponsesReasoningAndText(t, captured)
	requireResponsesInclude(t, captured["include"])
}

func TestDriverStreamRejectsResponsesStopSequences(t *testing.T) {
	driver := New(Config{WireAPI: WireResponses})
	stream, err := driver.Stream(context.Background(), provider.Request{
		StopSequences: []string{"stop"},
	})
	if stream != nil {
		_ = stream.Close()
		t.Fatal("Stream() returned a stream with unsupported stop sequences")
	}
	if err == nil || !strings.Contains(err.Error(), "does not support stop sequences") {
		t.Fatalf("Stream() error = %v, want stop-sequence error", err)
	}
}

func requireResponsesInput(t *testing.T, value any) {
	t.Helper()
	input, ok := value.([]any)
	if !ok || len(input) != 5 {
		t.Fatalf("input = %#v, want five Responses items", value)
	}
	system, _ := input[0].(map[string]any)
	if system["role"] != "system" || system["content"] != "follow instructions" {
		t.Fatalf("system input = %#v", system)
	}
	assistant, _ := input[2].(map[string]any)
	if assistant["role"] != "assistant" || assistant["content"] != "I will check." {
		t.Fatalf("assistant input = %#v", assistant)
	}
	call, _ := input[3].(map[string]any)
	if call["type"] != "function_call" || call["call_id"] != "call_1" || call["name"] != "lookup" {
		t.Fatalf("function call input = %#v", call)
	}
	output, _ := input[4].(map[string]any)
	if output["type"] != "function_call_output" || output["call_id"] != "call_1" || output["output"] != "found" {
		t.Fatalf("function output input = %#v", output)
	}
}

func requireResponsesTools(t *testing.T, value any) {
	t.Helper()
	tools, ok := value.([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want one flat tool", value)
	}
	tool, _ := tools[0].(map[string]any)
	if tool["type"] != "function" || tool["name"] != "lookup" || tool["description"] != "Look up a project" {
		t.Fatalf("tool = %#v", tool)
	}
	if _, nested := tool["function"]; nested {
		t.Fatalf("tool unexpectedly used Chat Completions envelope: %#v", tool)
	}
	parameters, _ := tool["parameters"].(map[string]any)
	if parameters["type"] != "object" {
		t.Fatalf("tool parameters = %#v", parameters)
	}
}

func requireResponsesInclude(t *testing.T, value any) {
	t.Helper()
	include, ok := value.([]any)
	if !ok || len(include) != 2 {
		t.Fatalf("include = %#v, want required reasoning plus caller value", value)
	}
	if include[0] != responsesEncryptedReasoningInclude || include[1] != "message.output_text.logprobs" {
		t.Fatalf("include = %#v", include)
	}
}

func requireResponsesReasoningAndText(t *testing.T, captured map[string]any) {
	t.Helper()
	reasoning, _ := captured["reasoning"].(map[string]any)
	if reasoning["effort"] != "medium" || reasoning["summary"] != "auto" {
		t.Fatalf("reasoning = %#v", reasoning)
	}
	text, _ := captured["text"].(map[string]any)
	format, _ := text["format"].(map[string]any)
	if format["type"] != "json_schema" || format["name"] != "report" || format["strict"] != true {
		t.Fatalf("text.format = %#v", format)
	}
	if _, ok := format["schema"].(map[string]any); !ok {
		t.Fatalf("text.format.schema = %#v", format["schema"])
	}
}

func TestDriverStreamResponsesUsesSharedHTTPHandling(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			http.Error(writer, "retry", http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"output\":[],\"usage\":{}}}\n\n"))
	}))
	defer server.Close()

	driver := New(Config{
		APIKey:  "test",
		BaseURL: server.URL,
		Client:  server.Client(),
		WireAPI: WireResponses,
		Retry: shared.RetryPolicy{
			MaxAttempts: 2,
			BaseDelay:   time.Nanosecond,
			MaxDelay:    time.Nanosecond,
		},
	})
	stream, err := driver.Stream(context.Background(), provider.Request{Model: "gpt-5-codex"})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	_ = collectEvents(t, stream)
	if attempts.Load() != 2 {
		t.Fatalf("attempts = %d, want 2", attempts.Load())
	}
}

func TestDriverStreamReplaysResponsesProviderStateBeforeToolOutput(t *testing.T) {
	var captured struct {
		Input []json.RawMessage `json:"input"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&captured); err != nil {
			t.Errorf("decode request: %v", err)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"output\":[],\"usage\":{}}}\n\n"))
	}))
	defer server.Close()

	state := json.RawMessage(`[{"id":"rs_1","type":"reasoning","encrypted_content":"opaque"},{"id":"msg_1","type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Checking","annotations":[]}]},{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"query\":\"hydaelyn\"}"}]`)
	driver := New(Config{
		APIKey:  "test",
		BaseURL: server.URL,
		Client:  server.Client(),
		WireAPI: WireResponses,
	})
	stream, err := driver.Stream(context.Background(), provider.Request{
		Model: "gpt-5-codex",
		Messages: []message.Message{
			message.NewText(message.RoleUser, "look it up"),
			{
				Role:          message.RoleAssistant,
				Text:          "normalized text must not replace saved output",
				ProviderState: state,
				ToolCalls: []message.ToolCall{{
					ID:        "call_1",
					Name:      "lookup",
					Arguments: json.RawMessage(`{"query":"duplicate"}`),
				}},
			},
			message.NewToolResult(message.ToolResult{
				ToolCallID: "call_1",
				Name:       "lookup",
				Content:    "found",
			}),
		},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	_ = collectEvents(t, stream)

	var expected []json.RawMessage
	if err := json.Unmarshal(state, &expected); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(captured.Input) != len(expected)+2 {
		t.Fatalf("input = %#v, want user + provider output + tool output", captured.Input)
	}
	for index := range expected {
		if !bytes.Equal(captured.Input[index+1], expected[index]) {
			t.Fatalf("input[%d] = %s, want exact provider item %s", index+1, captured.Input[index+1], expected[index])
		}
	}
	var toolOutput map[string]any
	if err := json.Unmarshal(captured.Input[len(captured.Input)-1], &toolOutput); err != nil {
		t.Fatalf("decode tool output: %v", err)
	}
	if toolOutput["type"] != "function_call_output" || toolOutput["call_id"] != "call_1" {
		t.Fatalf("tool output = %#v", toolOutput)
	}
}

func TestDriverStreamRejectsInvalidResponsesProviderState(t *testing.T) {
	driver := New(Config{APIKey: "test", WireAPI: WireResponses})
	stream, err := driver.Stream(context.Background(), provider.Request{
		Messages: []message.Message{{
			Role:          message.RoleAssistant,
			ProviderState: json.RawMessage(`{"type":"reasoning"}`),
		}},
	})
	if stream != nil {
		_ = stream.Close()
		t.Fatal("Stream() returned a stream for invalid provider state")
	}
	if err == nil || !strings.Contains(err.Error(), "provider state must be a JSON array") {
		t.Fatalf("Stream() error = %v, want JSON array validation error", err)
	}
}

func TestResponsesStreamDecodesTypedEvents(t *testing.T) {
	stream := newResponsesTestStream(`data: {"type":"response.created","response":{"output":[]}}

data: {"type":"response.unknown_progress","sequence_number":1}

data: {"type":"response.output_item.added","output_index":0,"item":{"id":"msg_commentary","type":"message","phase":"commentary"}}

data: {"type":"response.output_text.delta","output_index":0,"delta":"Checking"}

data: {"type":"response.output_item.added","output_index":1,"item":{"id":"rs_1","type":"reasoning"}}

data: {"type":"response.reasoning_summary_text.delta","output_index":1,"delta":"Plan"}

data: {"type":"response.reasoning_text.delta","output_index":1,"delta":" raw"}

data: {"type":"response.output_item.added","output_index":2,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":""}}

data: {"type":"response.function_call_arguments.delta","output_index":2,"delta":"{\"query\":\"hy"}

data: {"type":"response.function_call_arguments.delta","output_index":2,"delta":"daelyn\"}"}

data: {"type":"response.output_item.done","output_index":2,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"query\":\"hydaelyn\"}"}}

data: {"type":"response.output_item.added","output_index":3,"item":{"id":"msg_final","type":"message","phase":"final_answer"}}

data: {"type":"response.output_text.delta","output_index":3,"delta":"Answer"}

data: {"type":"response.refusal.delta","output_index":3,"delta":" refused"}

data: {"type":"response.completed","response":{"output":[{"id":"rs_1","type":"reasoning","encrypted_content":"opaque"},{"id":"msg_commentary","type":"message","phase":"commentary"},{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"query\":\"hydaelyn\"}"},{"id":"msg_final","type":"message","phase":"final_answer"}],"usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}}

`)
	events := collectEvents(t, stream)
	if len(events) != 8 {
		t.Fatalf("events = %#v, want 8 semantic events", events)
	}
	if events[0].Kind != provider.EventTextDelta || events[0].Text != "Checking" || events[0].TextPhase != provider.TextPhaseCommentary {
		t.Fatalf("commentary event = %#v", events[0])
	}
	if events[1].Kind != provider.EventThinkingDelta || events[1].Thinking != "Plan" {
		t.Fatalf("reasoning summary event = %#v", events[1])
	}
	if events[2].Kind != provider.EventThinkingDelta || events[2].Thinking != " raw" {
		t.Fatalf("raw reasoning event = %#v", events[2])
	}
	for index, event := range events[3:5] {
		if event.Kind != provider.EventToolCallDelta || event.ToolCallDelta == nil {
			t.Fatalf("tool delta %d = %#v", index, event)
		}
		if event.ToolCallDelta.Index == nil || *event.ToolCallDelta.Index != 2 {
			t.Fatalf("tool delta index = %#v", event.ToolCallDelta)
		}
		if event.ToolCallDelta.ID != "call_1" || event.ToolCallDelta.Name != "lookup" {
			t.Fatalf("tool delta identity = %#v", event.ToolCallDelta)
		}
	}
	if events[5].TextPhase != provider.TextPhaseFinalAnswer || events[5].Text != "Answer" {
		t.Fatalf("final text event = %#v", events[5])
	}
	if events[6].TextPhase != provider.TextPhaseFinalAnswer || events[6].Text != " refused" {
		t.Fatalf("refusal event = %#v", events[6])
	}
	done := events[7]
	if done.Kind != provider.EventDone || done.StopReason != provider.StopReasonToolUse {
		t.Fatalf("done event = %#v", done)
	}
	if done.Usage != (provider.Usage{InputTokens: 11, OutputTokens: 7, TotalTokens: 18}) {
		t.Fatalf("usage = %#v", done.Usage)
	}
	wantState := `[{"id":"rs_1","type":"reasoning","encrypted_content":"opaque"},{"id":"msg_commentary","type":"message","phase":"commentary"},{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"query\":\"hydaelyn\"}"},{"id":"msg_final","type":"message","phase":"final_answer"}]`
	if string(done.ProviderState) != wantState {
		t.Fatalf("provider state = %s, want %s", done.ProviderState, wantState)
	}
	normalized, err := provider.NormalizeEvents(events)
	if err != nil {
		t.Fatalf("NormalizeEvents() error = %v", err)
	}
	if normalized.Text != "CheckingAnswer refused" || normalized.Thinking != "Plan raw" {
		t.Fatalf("normalized response = %#v", normalized)
	}
	if len(normalized.ToolCalls) != 1 || string(normalized.ToolCalls[0].Arguments) != `{"query":"hydaelyn"}` {
		t.Fatalf("normalized tool calls = %#v", normalized.ToolCalls)
	}
}

func TestResponsesStreamFallsBackToCompletedFunctionCall(t *testing.T) {
	stream := newResponsesTestStream(`data: {"type":"response.output_item.added","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":""}}

data: {"type":"response.output_item.done","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"query\":\"hydaelyn\"}"}}

data: {"type":"response.completed","response":{"output":[{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"query\":\"hydaelyn\"}"}],"usage":{}}}

`)
	events := collectEvents(t, stream)
	if len(events) != 2 || events[0].Kind != provider.EventToolCallDelta {
		t.Fatalf("events = %#v, want fallback tool delta and done", events)
	}
	delta := events[0].ToolCallDelta
	if delta == nil || delta.ID != "call_1" || delta.Name != "lookup" || delta.ArgumentsDelta != `{"query":"hydaelyn"}` {
		t.Fatalf("fallback delta = %#v", delta)
	}
}

func TestResponsesStreamMapsIncompleteReasons(t *testing.T) {
	tests := []struct {
		name       string
		reason     string
		wantReason provider.StopReason
	}{
		{name: "output limit", reason: "max_output_tokens", wantReason: provider.StopReasonMaxTurns},
		{name: "other reason", reason: "content_filter", wantReason: provider.StopReasonUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stream := newResponsesTestStream(`data: {"type":"response.incomplete","response":{"output":[],"incomplete_details":{"reason":"` + test.reason + `"},"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7}}}

`)
			events := collectEvents(t, stream)
			if len(events) != 1 || events[0].Kind != provider.EventDone || events[0].StopReason != test.wantReason {
				t.Fatalf("events = %#v, want stop reason %q", events, test.wantReason)
			}
			if events[0].Usage.TotalTokens != 7 || string(events[0].ProviderState) != "[]" {
				t.Fatalf("terminal event = %#v", events[0])
			}
		})
	}
}

func TestResponsesStreamSurfacesAPIErrorEvents(t *testing.T) {
	tests := []struct {
		name string
		sse  string
		want string
	}{
		{
			name: "failed response",
			sse:  "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"model_error\",\"message\":\"generation failed\"}}}\n\n",
			want: "model_error",
		},
		{
			name: "top-level error",
			sse:  "data: {\"type\":\"error\",\"code\":\"server_error\",\"message\":\"try later\"}\n\n",
			want: "server_error",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			events := collectEvents(t, newResponsesTestStream(test.sse))
			if len(events) != 1 || events[0].Kind != provider.EventError || events[0].Err == nil {
				t.Fatalf("events = %#v, want one error event", events)
			}
			if !strings.Contains(events[0].Err.Error(), test.want) {
				t.Fatalf("error = %v, want code %q", events[0].Err, test.want)
			}
		})
	}
}

func TestResponsesStreamRejectsMalformedJSON(t *testing.T) {
	stream := newResponsesTestStream("data: {not-json}\n\n")
	defer func() { _ = stream.Close() }()
	if _, err := stream.Recv(); err == nil || !strings.Contains(err.Error(), "decode openai responses stream event") {
		t.Fatalf("Recv() error = %v, want malformed JSON error", err)
	}
}

func TestDriverResponsesTwoTurnToolLoop(t *testing.T) {
	firstOutput := `[{"id":"rs_1","type":"reasoning","encrypted_content":"opaque"},{"id":"msg_commentary","type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Checking.","annotations":[]}]},{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"query\":\"hydaelyn\"}"}]`
	secondOutput := `[{"id":"msg_final","type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Done.","annotations":[]}]}]`
	var (
		attempts atomic.Int32
		mu       sync.Mutex
		paths    []string
		bodies   [][]byte
	)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		mu.Lock()
		paths = append(paths, request.URL.Path)
		bodies = append(bodies, append([]byte(nil), body...))
		mu.Unlock()

		writer.Header().Set("Content-Type", "text/event-stream")
		if attempts.Add(1) == 1 {
			_, _ = writer.Write([]byte("data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"msg_commentary\",\"type\":\"message\",\"phase\":\"commentary\"}}\n\n"))
			_, _ = writer.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"delta\":\"Checking.\"}\n\n"))
			_, _ = writer.Write([]byte("data: {\"type\":\"response.output_item.added\",\"output_index\":1,\"item\":{\"id\":\"rs_1\",\"type\":\"reasoning\"}}\n\n"))
			_, _ = writer.Write([]byte("data: {\"type\":\"response.output_item.added\",\"output_index\":2,\"item\":{\"id\":\"fc_1\",\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"lookup\"}}\n\n"))
			_, _ = writer.Write([]byte("data: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":2,\"delta\":\"{\\\"query\\\":\\\"hydaelyn\\\"}\"}\n\n"))
			_, _ = writer.Write([]byte("data: {\"type\":\"response.output_item.done\",\"output_index\":2,\"item\":{\"id\":\"fc_1\",\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"arguments\":\"{\\\"query\\\":\\\"hydaelyn\\\"}\"}}\n\n"))
			_, _ = writer.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"output\":" + firstOutput + ",\"usage\":{\"input_tokens\":8,\"output_tokens\":5,\"total_tokens\":13}}}\n\n"))
			return
		}
		_, _ = writer.Write([]byte("data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"msg_final\",\"type\":\"message\",\"phase\":\"final_answer\"}}\n\n"))
		_, _ = writer.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"delta\":\"Done.\"}\n\n"))
		_, _ = writer.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"output\":" + secondOutput + ",\"usage\":{\"input_tokens\":16,\"output_tokens\":2,\"total_tokens\":18}}}\n\n"))
	}))
	defer server.Close()

	driver := New(Config{
		APIKey:  "test",
		BaseURL: server.URL,
		Client:  server.Client(),
		WireAPI: WireResponses,
	})
	engine := agent.Engine{
		Provider: driver,
		Tools:    tool.NewBus(responsesSmokeTool{}),
	}
	result, err := engine.RunMessages(context.Background(), agent.LoopInput{
		Model:         "gpt-5-codex",
		Messages:      []message.Message{message.NewText(message.RoleUser, "look it up")},
		MaxIterations: 2,
	})
	if err != nil {
		t.Fatalf("RunMessages() error = %v", err)
	}
	if result.StopReason != provider.StopReasonComplete {
		t.Fatalf("StopReason = %q, want complete", result.StopReason)
	}
	if attempts.Load() != 2 {
		t.Fatalf("requests = %d, want two model turns", attempts.Load())
	}
	last := result.Messages[len(result.Messages)-1]
	if last.Text != "Done." || string(last.ProviderState) != secondOutput {
		t.Fatalf("final assistant = %#v", last)
	}

	mu.Lock()
	capturedPaths := append([]string(nil), paths...)
	capturedBodies := append([][]byte(nil), bodies...)
	mu.Unlock()
	if len(capturedPaths) != 2 || capturedPaths[0] != "/responses" || capturedPaths[1] != "/responses" {
		t.Fatalf("paths = %#v, want two /responses calls", capturedPaths)
	}
	var secondRequest struct {
		Input []json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(capturedBodies[1], &secondRequest); err != nil {
		t.Fatalf("decode second request: %v", err)
	}
	var expectedReplay []json.RawMessage
	if err := json.Unmarshal([]byte(firstOutput), &expectedReplay); err != nil {
		t.Fatalf("decode first output fixture: %v", err)
	}
	if len(secondRequest.Input) != len(expectedReplay)+2 {
		t.Fatalf("second input = %#v, want user + exact output + tool result", secondRequest.Input)
	}
	for index := range expectedReplay {
		if !bytes.Equal(secondRequest.Input[index+1], expectedReplay[index]) {
			t.Fatalf("second input[%d] = %s, want %s", index+1, secondRequest.Input[index+1], expectedReplay[index])
		}
	}
	var toolOutput map[string]any
	if err := json.Unmarshal(secondRequest.Input[len(secondRequest.Input)-1], &toolOutput); err != nil {
		t.Fatalf("decode second-turn tool output: %v", err)
	}
	if toolOutput["type"] != "function_call_output" || toolOutput["call_id"] != "call_1" || toolOutput["output"] != "found" {
		t.Fatalf("second-turn tool output = %#v", toolOutput)
	}
}

type responsesSmokeTool struct{}

func (responsesSmokeTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        "lookup",
		Description: "Look up a project",
		InputSchema: tool.Schema{
			Type: "object",
			Properties: map[string]tool.Schema{
				"query": {Type: "string"},
			},
			Required: []string{"query"},
		},
	}
}

func (responsesSmokeTool) Execute(_ context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	return tool.Result{
		ToolCallID: call.ID,
		Name:       call.Name,
		Content:    "found",
	}, nil
}

func newResponsesTestStream(sse string) *responsesStream {
	body := io.NopCloser(strings.NewReader(sse))
	return &responsesStream{
		body:   body,
		reader: shared.NewReader(body),
		items:  make(map[int]*responsesOutputState),
	}
}
