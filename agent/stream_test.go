package agent

import (
	"context"
	"testing"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/stream"
	"github.com/Viking602/venat/tool"
	"github.com/Viking602/venat/tool/kit"
)

func TestEngineStreamsFramesWithoutChangingResult(t *testing.T) {
	driver, err := kit.Tool("lookup", func(_ context.Context, input struct {
		Query string `json:"query"`
	},
	) (string, error) {
		return "result:" + input.Query, nil
	})
	if err != nil {
		t.Fatalf("tool setup: %v", err)
	}
	engine := Engine{Provider: fakeProvider{}, Tools: tool.NewBus(driver)}

	var kinds []stream.FrameKind
	acc := stream.NewAccumulator()
	sink := stream.NewBroadcast(
		stream.SinkFunc(func(_ context.Context, frame stream.Frame) error {
			kinds = append(kinds, frame.Kind)
			return nil
		}),
		acc,
	)

	input := LoopInput{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "find venat")},
		MaxIterations: 3,
		ToolMode:      tool.ModeSequential,
		Sink:          sink,
	}
	streamed, err := engine.RunMessages(context.Background(), input)
	if err != nil {
		t.Fatalf("RunMessages(stream) error = %v", err)
	}

	// The live stream must observe the tool call, the tool result
	// enrichment, and the final text, across both turns.
	want := []stream.FrameKind{
		stream.FrameToolCall, stream.FrameDone,
		stream.FrameToolResult,
		stream.FrameText, stream.FrameDone,
	}
	if len(kinds) != len(want) {
		t.Fatalf("frame kinds = %#v, want %#v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("frame[%d] = %q, want %q (all = %#v)", i, kinds[i], want[i], kinds)
		}
	}

	// Final-state-only durability: running without a sink yields the same
	// terminal output the stream consumer could fold for itself.
	plain, err := engine.RunMessages(context.Background(), LoopInput{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "find venat")},
		MaxIterations: 3,
		ToolMode:      tool.ModeSequential,
	})
	if err != nil {
		t.Fatalf("RunMessages(plain) error = %v", err)
	}
	if streamed.StopReason != plain.StopReason {
		t.Fatalf("stop reason streamed=%q plain=%q", streamed.StopReason, plain.StopReason)
	}
	last := streamed.Messages[len(streamed.Messages)-1].Text
	if last != plain.Messages[len(plain.Messages)-1].Text {
		t.Fatalf("final text streamed=%q plain=%q", last, plain.Messages[len(plain.Messages)-1].Text)
	}

	folded, err := acc.Message()
	if err != nil {
		t.Fatalf("accumulator Message error = %v", err)
	}
	if folded.ToolCalls[0].Name != "lookup" {
		t.Fatalf("accumulated tool call = %#v", folded.ToolCalls)
	}
}
