package stream

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"testing"

	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
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

func TestChannelProducerConsumer(t *testing.T) {
	ctx := context.Background()
	ch := NewChannel(2)
	go func() {
		_ = ch.Emit(ctx, Frame{Kind: FrameText, Text: "a"})
		_ = ch.Emit(ctx, Frame{Kind: FrameText, Text: "b"})
		ch.Close()
	}()

	var got string
	for frame := range ch.Seq() {
		got += frame.Text
	}
	if got != "ab" {
		t.Fatalf("ranged frames = %q, want ab", got)
	}
	if err := ch.Emit(ctx, Frame{Kind: FrameText}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Emit after Close error = %v, want ErrClosed", err)
	}
}

func TestChannelEmitRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := NewChannel(0)
	cancel()
	if err := ch.Emit(ctx, Frame{Kind: FrameText}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Emit on cancelled ctx error = %v, want context.Canceled", err)
	}
}

func TestMergeFansInAndStampsSource(t *testing.T) {
	ctx := context.Background()
	a := make(chan Frame, 2)
	b := make(chan Frame, 2)
	a <- Frame{Kind: FrameText, Text: "a1"}
	a <- Frame{Kind: FrameText, Text: "a2"}
	close(a)
	b <- Frame{Kind: FrameText, Text: "b1"}
	close(b)

	var (
		mu    sync.Mutex
		bySrc = map[string][]string{}
	)
	dst := SinkFunc(func(_ context.Context, frame Frame) error {
		mu.Lock()
		defer mu.Unlock()
		bySrc[frame.Source] = append(bySrc[frame.Source], frame.Text)
		return nil
	})

	if err := Merge(ctx, dst, Source{Label: "agent-a", Frames: a}, Source{Label: "agent-b", Frames: b}); err != nil {
		t.Fatalf("Merge error = %v", err)
	}
	sort.Strings(bySrc["agent-a"])
	if len(bySrc["agent-a"]) != 2 || bySrc["agent-a"][0] != "a1" {
		t.Fatalf("agent-a frames = %#v", bySrc["agent-a"])
	}
	if len(bySrc["agent-b"]) != 1 || bySrc["agent-b"][0] != "b1" {
		t.Fatalf("agent-b frames = %#v", bySrc["agent-b"])
	}
}
