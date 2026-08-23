package provider

import (
	"errors"
	"testing"

	"github.com/Viking602/venat/message"
)

func TestNormalizeEventsRequiresOneFinalTerminalEvent(t *testing.T) {
	if _, err := NormalizeEvents([]Event{{Kind: EventTextDelta, Text: "partial"}}); !errors.Is(err, ErrMissingTerminalEvent) {
		t.Fatalf("missing terminal error = %v", err)
	}
	if _, err := NormalizeEvents([]Event{
		{Kind: EventDone, StopReason: StopReasonComplete},
		{Kind: EventDone, StopReason: StopReasonComplete},
	}); !errors.Is(err, ErrMultipleTerminalEvents) {
		t.Fatalf("duplicate terminal error = %v", err)
	}
	if _, err := NormalizeEvents([]Event{
		{Kind: EventDone, StopReason: StopReasonComplete},
		{Kind: EventTextDelta, Text: "late"},
	}); !errors.Is(err, ErrEventAfterTerminal) {
		t.Fatalf("post-terminal error = %v", err)
	}
}

func TestNormalizeEventsRejectsToolCallsWithoutStableIDs(t *testing.T) {
	_, err := NormalizeEvents([]Event{
		{Kind: EventToolCall, ToolCall: &message.ToolCall{Name: "lookup"}},
		{Kind: EventDone, StopReason: StopReasonToolUse},
	})
	if !errors.Is(err, ErrMissingToolCallID) {
		t.Fatalf("missing tool id error = %v", err)
	}
}

func TestNormalizeEventsRejectsToolIDIndexRebinding(t *testing.T) {
	first, second := 0, 1
	_, err := NormalizeEvents([]Event{
		{Kind: EventToolCallDelta, ToolCallDelta: &ToolCallDelta{ID: "call", Index: &first, Name: "lookup"}},
		{Kind: EventToolCallDelta, ToolCallDelta: &ToolCallDelta{ID: "call", Index: &second, ArgumentsDelta: `{}`}},
		{Kind: EventDone, StopReason: StopReasonToolUse},
	})
	if !errors.Is(err, ErrToolCallIdentityConflict) {
		t.Fatalf("tool id/index conflict error = %v", err)
	}
}

func TestNormalizePartialEventsKeepsInterruptedContent(t *testing.T) {
	response, err := NormalizePartialEvents([]Event{
		{Kind: EventThinkingDelta, Thinking: "plan", Signature: "sig"},
		{Kind: EventTextDelta, TextPhase: TextPhaseCommentary, Text: "checking"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Thinking != "plan" || response.Text != "checking" || len(response.Content) != 2 {
		t.Fatalf("partial response = %#v", response)
	}
}

func TestNormalizeEventsPreservesCommentaryAndFinalAnswerOrder(t *testing.T) {
	headers := map[string]string{"request-id": "req-1"}
	response, err := NormalizeEvents([]Event{
		{Kind: EventTextDelta, TextPhase: TextPhaseCommentary, Text: "checking"},
		{Kind: EventTextDelta, TextPhase: TextPhaseFinalAnswer, Text: "done"},
		{Kind: EventDone, StopReason: StopReasonComplete, Response: ResponseMetadata{ID: "resp-1", Model: "model-1", Headers: headers}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Content) != 2 || response.Content[0].Kind != message.ContentCommentary || response.Content[1].Kind != message.ContentFinalAnswer {
		t.Fatalf("ordered content = %#v", response.Content)
	}
	if response.Content[0].Text != "checking" || response.Content[1].Text != "done" {
		t.Fatalf("ordered text = %#v", response.Content)
	}
	headers["request-id"] = "changed"
	if response.Response.ID != "resp-1" || response.Response.Model != "model-1" || response.Response.Headers["request-id"] != "req-1" {
		t.Fatalf("response metadata = %#v", response.Response)
	}
}
