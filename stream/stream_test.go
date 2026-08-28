package stream

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
)

func TestFrameFromEventMapsEveryProviderKind(t *testing.T) {
	tests := []struct {
		name  string
		event provider.Event
		want  FrameKind
	}{
		{"text", provider.Event{Kind: provider.EventTextDelta, Text: "hi"}, FrameText},
		{"thinking", provider.Event{Kind: provider.EventThinkingDelta, Thinking: "hmm"}, FrameThinking},
		{"tool_call", provider.Event{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{ID: "t1"}}, FrameToolCall},
		{"tool_call_delta", provider.Event{Kind: provider.EventToolCallDelta, ToolCallDelta: &provider.ToolCallDelta{ID: "t1"}}, FrameToolCallDelta},
		{"done", provider.Event{Kind: provider.EventDone, StopReason: provider.StopReasonComplete}, FrameDone},
		{"error", provider.Event{Kind: provider.EventError, Err: errors.New("boom")}, FrameError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			frame, ok := FrameFromEvent(tc.event)
			if !ok {
				t.Fatalf("FrameFromEvent(%s) ok = false", tc.name)
			}
			if frame.Kind != tc.want {
				t.Fatalf("FrameFromEvent(%s) kind = %q, want %q", tc.name, frame.Kind, tc.want)
			}
		})
	}
}

func TestFrameRoundTripsTextPhaseAndProviderState(t *testing.T) {
	textEvent := provider.Event{
		Kind:      provider.EventTextDelta,
		Text:      "checking",
		TextPhase: provider.TextPhaseCommentary,
	}
	textFrame, ok := FrameFromEvent(textEvent)
	if !ok || textFrame.TextPhase != provider.TextPhaseCommentary {
		t.Fatalf("text frame = %#v, ok = %v", textFrame, ok)
	}
	gotText, ok := textFrame.ToEvent()
	if !ok || gotText.TextPhase != provider.TextPhaseCommentary {
		t.Fatalf("round-tripped text event = %#v, ok = %v", gotText, ok)
	}

	state := json.RawMessage(`[{"type":"reasoning","id":"rs_1"}]`)
	doneFrame, ok := FrameFromEvent(provider.Event{
		Kind:          provider.EventDone,
		StopReason:    provider.StopReasonComplete,
		ProviderState: state,
	})
	if !ok || string(doneFrame.ProviderState) != string(state) {
		t.Fatalf("done frame = %#v, ok = %v", doneFrame, ok)
	}
	gotDone, ok := doneFrame.ToEvent()
	if !ok || string(gotDone.ProviderState) != string(state) {
		t.Fatalf("round-tripped done event = %#v, ok = %v", gotDone, ok)
	}
}

func TestFrameFromEventRejectsUnknownKind(t *testing.T) {
	if _, ok := FrameFromEvent(provider.Event{Kind: "nonsense"}); ok {
		t.Fatal("FrameFromEvent should report ok=false for an unknown kind")
	}
}

func TestAccumulatorFoldsStreamIntoFinalMessage(t *testing.T) {
	acc := NewAccumulator()
	ctx := context.Background()
	frames := []Frame{
		{Kind: FrameText, Text: "Hello, "},
		{Kind: FrameText, Text: "world"},
		{Kind: FrameToolCall, ToolCall: &message.ToolCall{ID: "t1", Name: "lookup", Arguments: json.RawMessage(`{"q":"x"}`)}},
		{Kind: FrameToolResult, ToolResult: &message.ToolResult{ToolCallID: "t1", Name: "lookup", Content: "ok"}},
		{Kind: FrameDone, StopReason: provider.StopReasonComplete, Usage: provider.Usage{OutputTokens: 3}},
	}
	for _, frame := range frames {
		if err := acc.Emit(ctx, frame); err != nil {
			t.Fatalf("Emit error = %v", err)
		}
	}

	msg, err := acc.Message()
	if err != nil {
		t.Fatalf("Message error = %v", err)
	}
	if msg.Text != "Hello, world" {
		t.Fatalf("accumulated text = %q", msg.Text)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Name != "lookup" {
		t.Fatalf("accumulated tool calls = %#v", msg.ToolCalls)
	}
	results := acc.ToolResults()
	if len(results) != 1 || results[0].Content != "ok" {
		t.Fatalf("accumulated tool results = %#v", results)
	}
}

func TestBroadcastDeliversToAllSinksAndJoinsErrors(t *testing.T) {
	ctx := context.Background()
	var got int
	good := SinkFunc(func(_ context.Context, _ Frame) error {
		got++
		return nil
	})
	bad := SinkFunc(func(_ context.Context, _ Frame) error {
		return errors.New("sink failed")
	})
	another := NewAccumulator()

	b := NewBroadcast(good, bad, another, nil)
	err := b.Emit(ctx, Frame{Kind: FrameText, Text: "x"})
	if err == nil {
		t.Fatal("Broadcast.Emit should surface the failing sink error")
	}
	if got != 1 {
		t.Fatalf("healthy sink calls = %d, want 1 (one failing sink must not starve others)", got)
	}
	msg, _ := another.Message()
	if msg.Text != "x" {
		t.Fatalf("third sink missed the frame: %q", msg.Text)
	}
}

// TestThinkingSignatureRoundTripsThroughFrame is the regression for the
// streaming-path signature loss: Anthropic emits signature_delta and
// redacted_thinking as provider.EventThinkingDelta carrying Signature /
// RedactedThinking. Before the fix, Frame dropped those fields, so the
// accumulator's normalized response (and the message built from it) had
// empty signature — and the next assistant turn was rejected by the API.
// The round-trip FrameFromEvent → ToEvent → NormalizeEvents must preserve
// both payloads, and Accumulator.Message must carry them onto the message.
func TestThinkingSignatureRoundTripsThroughFrame(t *testing.T) {
	events := []provider.Event{
		{Kind: provider.EventThinkingDelta, Thinking: "reasoning"},
		{Kind: provider.EventThinkingDelta, Signature: "sig-abc"},
		{Kind: provider.EventThinkingDelta, RedactedThinking: "enc-123"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}

	// Frame path: each event converts to a Frame and back without loss.
	var acc []provider.Event
	for _, event := range events {
		frame, ok := FrameFromEvent(event)
		if !ok {
			t.Fatalf("FrameFromEvent(%s) ok = false", event.Kind)
		}
		round, ok := frame.ToEvent()
		if !ok {
			t.Fatalf("ToEvent(%s) ok = false", frame.Kind)
		}
		acc = append(acc, round)
	}
	normalized, err := provider.NormalizeEvents(acc)
	if err != nil {
		t.Fatalf("NormalizeEvents error = %v", err)
	}
	if normalized.Signature != "sig-abc" {
		t.Fatalf("normalized.Signature = %q, want sig-abc", normalized.Signature)
	}
	if normalized.RedactedThinking != "enc-123" {
		t.Fatalf("normalized.RedactedThinking = %q, want enc-123", normalized.RedactedThinking)
	}

	// Accumulator path: Emit frames and read the message — signature and
	// redacted thinking must land on message.Message for the next turn.
	sink := NewAccumulator()
	for _, event := range events {
		frame, _ := FrameFromEvent(event)
		if err := sink.Emit(context.Background(), frame); err != nil {
			t.Fatalf("Accumulator.Emit error = %v", err)
		}
	}
	msg, err := sink.Message()
	if err != nil {
		t.Fatalf("Accumulator.Message error = %v", err)
	}
	if msg.ThinkingSignature != "sig-abc" {
		t.Fatalf("message.ThinkingSignature = %q, want sig-abc", msg.ThinkingSignature)
	}
	if msg.RedactedThinking != "enc-123" {
		t.Fatalf("message.RedactedThinking = %q, want enc-123", msg.RedactedThinking)
	}
}
