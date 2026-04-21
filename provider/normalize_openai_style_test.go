package provider

import "testing"

func intPtr(v int) *int {
	return &v
}

func TestNormalizeEventsOpenAIStyleToolCallDeltaWithoutRepeatedID(t *testing.T) {
	t.Parallel()

	response, err := NormalizeEvents([]Event{
		{
			Kind: EventToolCallDelta,
			ToolCallDelta: &ToolCallDelta{
				Index:          intPtr(0),
				ID:             "call-1",
				Name:           "lookup",
				ArgumentsDelta: `{"query":"hy`,
			},
		},
		{
			Kind: EventToolCallDelta,
			ToolCallDelta: &ToolCallDelta{
				Index:          intPtr(0),
				ArgumentsDelta: `daelyn"}`,
			},
		},
		{Kind: EventDone, StopReason: StopReasonToolUse},
	})
	if err != nil {
		t.Fatalf("NormalizeEvents() error = %v", err)
	}
	if len(response.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %#v", response.ToolCalls)
	}
	if response.ToolCalls[0].ID != "call-1" {
		t.Fatalf("expected tool call id call-1, got %#v", response.ToolCalls[0])
	}
	if response.ToolCalls[0].Name != "lookup" {
		t.Fatalf("expected tool call name lookup, got %#v", response.ToolCalls[0])
	}
	if string(response.ToolCalls[0].Arguments) != `{"query":"hydaelyn"}` {
		t.Fatalf("expected merged arguments, got %#v", response.ToolCalls[0])
	}
}
