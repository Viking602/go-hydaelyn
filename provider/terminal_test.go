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
	response, err := NormalizeEvents([]Event{
		{Kind: EventTextDelta, TextPhase: TextPhaseCommentary, Text: "checking"},
		{Kind: EventTextDelta, TextPhase: TextPhaseFinalAnswer, Text: "done"},
		{Kind: EventDone, StopReason: StopReasonComplete},
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
}
