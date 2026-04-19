package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/tool"
	"github.com/Viking602/go-hydaelyn/toolkit"
)

type fakeProvider struct{}

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

func TestEngineFailsWhenToolCallsExistButToolBusMissing(t *testing.T) {
	engine := Engine{Provider: fakeProvider{}}
	_, err := engine.Run(context.Background(), Input{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "find hydaelyn")},
		MaxIterations: 1,
	})
	if !errors.Is(err, ErrToolBusMissing) {
		t.Fatalf("expected ErrToolBusMissing, got %v", err)
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

func TestEngineCollectsThinkingDeltas(t *testing.T) {
	driver := &scriptedProvider{
		turns: [][]provider.Event{{
			{Kind: provider.EventThinkingDelta, Thinking: "thought-1"},
			{Kind: provider.EventThinkingDelta, Thinking: ";thought-2"},
			{Kind: provider.EventTextDelta, Text: "final answer"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}},
	}
	engine := Engine{Provider: driver}
	result, err := engine.Run(context.Background(), Input{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "hi")},
		MaxIterations: 1,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Thinking != "thought-1;thought-2" {
		t.Fatalf("expected accumulated thinking on Result, got %q", result.Thinking)
	}
	last := result.Messages[len(result.Messages)-1]
	if last.Thinking != "thought-1;thought-2" {
		t.Fatalf("expected thinking on assistant message, got %q", last.Thinking)
	}
	if last.Text != "final answer" {
		t.Fatalf("expected text answer, got %q", last.Text)
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

func TestEngineAccumulatesUsageAcrossTurns(t *testing.T) {
	driver := &scriptedProvider{
		turns: [][]provider.Event{
			{
				{
					Kind: provider.EventToolCall,
					ToolCall: &message.ToolCall{
						ID:        "call-1",
						Name:      "lookup",
						Arguments: json.RawMessage(`{"query":"hydaelyn"}`),
					},
				},
				{
					Kind:       provider.EventDone,
					StopReason: provider.StopReasonToolUse,
					Usage: provider.Usage{
						InputTokens:  11,
						OutputTokens: 7,
						TotalTokens:  18,
					},
				},
			},
			{
				{Kind: provider.EventTextDelta, Text: "done"},
				{
					Kind:       provider.EventDone,
					StopReason: provider.StopReasonComplete,
					Usage: provider.Usage{
						InputTokens:  5,
						OutputTokens: 3,
						TotalTokens:  8,
					},
				},
			},
		},
	}
	driverTool, err := toolkit.Tool("lookup", func(_ context.Context, input struct {
		Query string `json:"query"`
	}) (string, error) {
		return "result:" + input.Query, nil
	})
	if err != nil {
		t.Fatalf("tool setup: %v", err)
	}
	engine := Engine{
		Provider: driver,
		Tools:    tool.NewBus(driverTool),
	}
	result, err := engine.Run(context.Background(), Input{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "find hydaelyn")},
		MaxIterations: 3,
		ToolMode:      tool.ModeSequential,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Usage.InputTokens != 16 || result.Usage.OutputTokens != 10 || result.Usage.TotalTokens != 26 {
		t.Fatalf("expected accumulated usage, got %#v", result.Usage)
	}
}

func TestCollectBuildsToolCallsFromDeltasInStableOrder(t *testing.T) {
	engine := Engine{}
	assistant, _, _, err := engine.collect(context.Background(), provider.NewSliceStream([]provider.Event{
		{
			Kind: provider.EventToolCallDelta,
			ToolCallDelta: &provider.ToolCallDelta{
				ID:             "call-b",
				Name:           "beta",
				ArgumentsDelta: `{"value":"b"}`,
			},
		},
		{
			Kind: provider.EventToolCallDelta,
			ToolCallDelta: &provider.ToolCallDelta{
				ID:             "call-a",
				Name:           "alpha",
				ArgumentsDelta: `{"value":"a"}`,
			},
		},
		{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
	}), nil)
	if err != nil {
		t.Fatalf("collect() error = %v", err)
	}
	if len(assistant.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %#v", assistant.ToolCalls)
	}
	if assistant.ToolCalls[0].ID != "call-b" || assistant.ToolCalls[1].ID != "call-a" {
		t.Fatalf("expected stable tool call ordering, got %#v", assistant.ToolCalls)
	}
}

func TestCollectMergesFullAndDeltaToolCalls(t *testing.T) {
	engine := Engine{}
	assistant, _, _, err := engine.collect(context.Background(), provider.NewSliceStream([]provider.Event{
		{
			Kind: provider.EventToolCall,
			ToolCall: &message.ToolCall{
				ID:   "call-1",
				Name: "lookup",
			},
		},
		{
			Kind: provider.EventToolCallDelta,
			ToolCallDelta: &provider.ToolCallDelta{
				ID:             "call-1",
				ArgumentsDelta: `{"query":"hydaelyn"}`,
			},
		},
		{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
	}), nil)
	if err != nil {
		t.Fatalf("collect() error = %v", err)
	}
	if len(assistant.ToolCalls) != 1 {
		t.Fatalf("expected one merged tool call, got %#v", assistant.ToolCalls)
	}
	if got := string(assistant.ToolCalls[0].Arguments); got != `{"query":"hydaelyn"}` {
		t.Fatalf("expected merged arguments, got %q", got)
	}
}

func TestCollectRejectsInvalidToolCallJSON(t *testing.T) {
	engine := Engine{}
	_, _, _, err := engine.collect(context.Background(), provider.NewSliceStream([]provider.Event{
		{
			Kind: provider.EventToolCallDelta,
			ToolCallDelta: &provider.ToolCallDelta{
				ID:             "call-1",
				Name:           "lookup",
				ArgumentsDelta: `{"query":`,
			},
		},
		{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
	}), nil)
	if err == nil {
		t.Fatal("expected invalid tool call JSON error")
	}
}

func TestCollectRejectsDuplicateToolCallID(t *testing.T) {
	engine := Engine{}
	_, _, _, err := engine.collect(context.Background(), provider.NewSliceStream([]provider.Event{
		{
			Kind: provider.EventToolCall,
			ToolCall: &message.ToolCall{
				ID:        "call-1",
				Name:      "lookup",
				Arguments: json.RawMessage(`{"query":"one"}`),
			},
		},
		{
			Kind: provider.EventToolCall,
			ToolCall: &message.ToolCall{
				ID:        "call-1",
				Name:      "lookup",
				Arguments: json.RawMessage(`{"query":"two"}`),
			},
		},
		{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
	}), nil)
	if !errors.Is(err, provider.ErrDuplicateToolCallID) {
		t.Fatalf("expected duplicate tool call id error, got %v", err)
	}
}
