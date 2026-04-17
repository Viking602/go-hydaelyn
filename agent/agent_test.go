package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/middleware/formatter"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/tool"
	"github.com/Viking602/go-hydaelyn/toolkit"
)

type fakeProvider struct {
	streams []provider.Event
}

func (fakeProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "fake"}
}

func (f fakeProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	if len(request.Messages) >= 2 && request.Messages[len(request.Messages)-1].Role == message.RoleTool {
		return provider.NewSliceStream([]provider.Event{
			{Kind: provider.EventTextDelta, Text: "done"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}), nil
	}
	return provider.NewSliceStream([]provider.Event{
		{
			Kind: provider.EventToolCall,
			ToolCall: &message.ToolCall{
				ID:        "call-1",
				Name:      "lookup",
				Arguments: json.RawMessage(`{"query":"hydaelyn"}`),
			},
		},
		{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
	}), nil
}

func TestEngineRunsToolLoop(t *testing.T) {
	driver, err := toolkit.Tool("lookup", func(_ context.Context, input struct {
		Query string `json:"query"`
	}) (string, error) {
		return "result:" + input.Query, nil
	})
	if err != nil {
		t.Fatalf("tool setup: %v", err)
	}
	engine := Engine{
		Provider: fakeProvider{},
		Tools:    tool.NewBus(driver),
	}
	result, err := engine.Run(context.Background(), Input{
		Model: "test-model",
		Messages: []message.Message{
			message.NewText(message.RoleUser, "find hydaelyn"),
		},
		MaxIterations: 3,
		ToolMode:      tool.ModeSequential,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(result.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result.Messages))
	}
	if result.Messages[len(result.Messages)-1].Text != "done" {
		t.Fatalf("expected final assistant text, got %#v", result.Messages[len(result.Messages)-1])
	}
}

// scriptedProvider returns a pre-scripted event list per invocation, in
// order, so tests can drive Engine through multiple turns deterministically.
type scriptedProvider struct {
	turns     [][]provider.Event
	requests  []provider.Request
	callIndex int
}

func (*scriptedProvider) Metadata() provider.Metadata { return provider.Metadata{Name: "scripted"} }

func (s *scriptedProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	s.requests = append(s.requests, request)
	events := s.turns[s.callIndex]
	s.callIndex++
	return provider.NewSliceStream(events), nil
}

func TestEngineRetriesOnFormatViolation(t *testing.T) {
	driver := &scriptedProvider{
		turns: [][]provider.Event{
			{
				// First turn: two sentences in the first paragraph — violates spec.
				{Kind: provider.EventTextDelta, Text: "第一句。第二句。"},
				{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
			},
			{
				// Second turn (after retry message): single sentence, passes.
				{Kind: provider.EventTextDelta, Text: "合规的单句结论"},
				{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
			},
		},
	}
	spec := formatter.OutputSpec{FirstParagraphSingleSentence: true}

	var observed [][]formatter.Violation
	engine := Engine{Provider: driver}
	result, err := engine.Run(context.Background(), Input{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "hi")},
		MaxIterations: 4,
		OutputSpec:    &spec,
		MaxRetries:    2,
		OnRetry: func(v []formatter.Violation) {
			observed = append(observed, v)
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Retries != 1 {
		t.Fatalf("expected 1 retry, got %d", result.Retries)
	}
	if len(observed) != 1 || observed[0][0].Code != "first_paragraph_multi_sentence" {
		t.Fatalf("expected OnRetry called once with multi-sentence violation, got %#v", observed)
	}
	last := result.Messages[len(result.Messages)-1]
	if last.Text != "合规的单句结论" {
		t.Fatalf("expected final single-sentence answer, got %q", last.Text)
	}
	// Verify the retry message was injected before the second provider call.
	if len(driver.requests) != 2 {
		t.Fatalf("expected 2 provider calls, got %d", len(driver.requests))
	}
	secondMsgs := driver.requests[1].Messages
	var sawRetry bool
	for _, m := range secondMsgs {
		if m.Role == message.RoleUser && strings.Contains(m.Text, "不符合格式规范") {
			sawRetry = true
		}
	}
	if !sawRetry {
		t.Fatalf("expected retry user message in second turn, got %#v", secondMsgs)
	}
}

func TestEngineForwardsStopAndThinkingBudget(t *testing.T) {
	driver := &scriptedProvider{
		turns: [][]provider.Event{{
			{Kind: provider.EventTextDelta, Text: "ok"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}},
	}
	engine := Engine{Provider: driver}
	_, err := engine.Run(context.Background(), Input{
		Model:          "test-model",
		Messages:       []message.Message{message.NewText(message.RoleUser, "hi")},
		MaxIterations:  1,
		StopSequences:  []string{"Wait,"},
		ThinkingBudget: 3000,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(driver.requests) != 1 {
		t.Fatalf("expected 1 call, got %d", len(driver.requests))
	}
	req := driver.requests[0]
	if len(req.StopSequences) != 1 || req.StopSequences[0] != "Wait," {
		t.Fatalf("stop not forwarded, got %#v", req.StopSequences)
	}
	if req.ThinkingBudget != 3000 {
		t.Fatalf("thinking budget not forwarded, got %d", req.ThinkingBudget)
	}
}
