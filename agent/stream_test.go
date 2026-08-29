package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/tool"
	"github.com/Viking602/venat/tool/kit"
)

func TestEngineStreamsFramesWithoutChangingResult(t *testing.T) {
	driver, err := kit.Tool("lookup", func(_ context.Context, input struct {
		Query string `json:"query"`
	}, sink tool.UpdateSink,
	) (string, error) {
		result := "result:" + input.Query
		if err := sink(tool.Update{Kind: tool.UpdateProgress, Message: "looking up"}); err != nil {
			return "", err
		}
		if err := sink(tool.Update{Kind: tool.UpdateOutput, Parts: []message.ContentPart{message.TextPart(result)}}); err != nil {
			return "", err
		}
		return result, nil
	})
	if err != nil {
		t.Fatalf("tool setup: %v", err)
	}
	engine := Engine{Provider: fakeProvider{}, Tools: tool.NewBus(driver)}

	var kinds []FrameKind
	var frames []Frame
	acc := NewAccumulator()
	sink := NewBroadcast(
		SinkFunc(func(_ context.Context, frame Frame) error {
			kinds = append(kinds, frame.Kind)
			frames = append(frames, frame)
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
	want := []FrameKind{
		FrameToolCall, FrameDone,
		FrameToolUpdate, FrameToolUpdate, FrameToolResult,
		FrameText, FrameDone,
	}
	if len(kinds) != len(want) {
		t.Fatalf("frame kinds = %#v, want %#v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("frame[%d] = %q, want %q (all = %#v)", i, kinds[i], want[i], kinds)
		}
	}
	if frames[2].ToolUpdate == nil || frames[2].ToolUpdate.Kind != tool.UpdateProgress ||
		frames[2].ToolUpdate.ToolCallID != "call-1" || frames[2].ToolUpdate.OperationID != "turn:0:call:0" ||
		frames[2].ToolUpdate.Sequence != 1 {
		t.Fatalf("progress frame = %#v", frames[2])
	}
	if frames[3].ToolUpdate == nil || frames[3].ToolUpdate.Kind != tool.UpdateOutput ||
		frames[3].ToolUpdate.Sequence != 2 || len(frames[3].ToolUpdate.Parts) != 1 ||
		frames[3].ToolUpdate.Parts[0].Text != "result:venat" {
		t.Fatalf("output frame = %#v", frames[3])
	}
	if event, ok := frames[2].ToEvent(); ok {
		t.Fatalf("tool update mapped to provider event: %#v", event)
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
	if results := acc.ToolResults(); len(results) != 1 || results[0].Content != "result:venat" {
		t.Fatalf("accumulated tool results = %#v", results)
	}
}

func TestFrameFromToolUpdateClonesMutableContent(t *testing.T) {
	data := map[string]string{"phase": "before"}
	parts := []message.ContentPart{{
		Kind:         message.ContentImage,
		Data:         []byte{1, 2},
		ProviderData: json.RawMessage(`{"opaque":true}`),
		Source:       &message.Source{Title: "before"},
	}}
	frame := FrameFromToolUpdate(tool.Update{
		Kind:  tool.UpdateOutput,
		Data:  data,
		Parts: parts,
	})
	data["phase"] = "after"
	parts[0].Data[0] = 9
	parts[0].ProviderData[0] = '['
	parts[0].Source.Title = "after"

	if frame.Kind != FrameToolUpdate || frame.ToolUpdate == nil ||
		frame.ToolUpdate.Data["phase"] != "before" ||
		frame.ToolUpdate.Parts[0].Data[0] != 1 ||
		string(frame.ToolUpdate.Parts[0].ProviderData) != `{"opaque":true}` ||
		frame.ToolUpdate.Parts[0].Source.Title != "before" {
		t.Fatalf("cloned frame = %#v", frame)
	}
}
