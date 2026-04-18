package provider

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/Viking602/go-hydaelyn/message"
)

func TestUsage_Add(t *testing.T) {
	tests := []struct {
		name         string
		u1           Usage
		u2           Usage
		wantInput    int
		wantOutput   int
		wantTotal    int
	}{
		{
			name:       "add two usages",
			u1:         Usage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30},
			u2:         Usage{InputTokens: 5, OutputTokens: 10, TotalTokens: 15},
			wantInput:  15,
			wantOutput: 30,
			wantTotal:  45,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: Usage struct doesn't have an Add method, just testing struct fields
			u := Usage{
				InputTokens:  tt.u1.InputTokens + tt.u2.InputTokens,
				OutputTokens: tt.u1.OutputTokens + tt.u2.OutputTokens,
				TotalTokens:  tt.u1.TotalTokens + tt.u2.TotalTokens,
			}

			if u.InputTokens != tt.wantInput {
				t.Errorf("InputTokens = %v, want %v", u.InputTokens, tt.wantInput)
			}
			if u.OutputTokens != tt.wantOutput {
				t.Errorf("OutputTokens = %v, want %v", u.OutputTokens, tt.wantOutput)
			}
			if u.TotalTokens != tt.wantTotal {
				t.Errorf("TotalTokens = %v, want %v", u.TotalTokens, tt.wantTotal)
			}
		})
	}
}

func TestNewSliceStream(t *testing.T) {
	events := []Event{
		{Kind: EventTextDelta, Text: "Hello"},
		{Kind: EventTextDelta, Text: " World"},
		{Kind: EventDone},
	}

	stream := NewSliceStream(events)
	if stream == nil {
		t.Fatal("NewSliceStream() returned nil")
	}

	// Test receiving all events
	for i, want := range events {
		got, err := stream.Recv()
		if err != nil {
			t.Errorf("Recv() at %d error = %v", i, err)
			continue
		}
		if got.Kind != want.Kind {
			t.Errorf("Event %d Kind = %v, want %v", i, got.Kind, want.Kind)
		}
		if got.Text != want.Text {
			t.Errorf("Event %d Text = %v, want %v", i, got.Text, want.Text)
		}
	}

	// Test EOF after all events
	_, err := stream.Recv()
	if !errors.Is(err, io.EOF) {
		t.Errorf("After all events, Recv() error = %v, want io.EOF", err)
	}

	// Test Close
	if err := stream.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestNewSliceStream_Empty(t *testing.T) {
	stream := NewSliceStream([]Event{})

	_, err := stream.Recv()
	if !errors.Is(err, io.EOF) {
		t.Errorf("Recv() error = %v, want io.EOF", err)
	}
}

func TestEvent_Struct(t *testing.T) {
	toolCall := &message.ToolCall{
		ID:   "call-1",
		Name: "search",
	}

	toolCallDelta := &ToolCallDelta{
		ID:             "call-1",
		Name:           "search",
		ArgumentsDelta: "{\"q\":\"test\"}",
	}

	event := Event{
		Kind:          EventToolCall,
		Text:          "",
		Thinking:      "thought",
		ToolCall:      toolCall,
		ToolCallDelta: toolCallDelta,
		Usage:         Usage{InputTokens: 10},
		StopReason:    StopReasonComplete,
		Err:           nil,
	}

	if event.Kind != EventToolCall {
		t.Errorf("Kind = %v, want %v", event.Kind, EventToolCall)
	}
	if event.Thinking != "thought" {
		t.Errorf("Thinking = %v, want thought", event.Thinking)
	}
	if event.ToolCall == nil {
		t.Error("ToolCall should not be nil")
	}
	if event.ToolCallDelta == nil {
		t.Error("ToolCallDelta should not be nil")
	}
	if event.StopReason != StopReasonComplete {
		t.Errorf("StopReason = %v, want %v", event.StopReason, StopReasonComplete)
	}
}

func TestRequest_Struct(t *testing.T) {
	tools := []message.ToolDefinition{
		{Name: "search"},
	}

	req := Request{
		Model:          "gpt-4",
		Messages:       []message.Message{{Role: message.RoleUser, Text: "Hello"}},
		Tools:          tools,
		Metadata:       map[string]string{"key": "value"},
		StopSequences:  []string{"STOP"},
		ThinkingBudget: 1000,
	}

	if req.Model != "gpt-4" {
		t.Errorf("Model = %v, want gpt-4", req.Model)
	}
	if len(req.Messages) != 1 {
		t.Errorf("len(Messages) = %v, want 1", len(req.Messages))
	}
	if len(req.Tools) != 1 {
		t.Errorf("len(Tools) = %v, want 1", len(req.Tools))
	}
	if req.ThinkingBudget != 1000 {
		t.Errorf("ThinkingBudget = %v, want 1000", req.ThinkingBudget)
	}
}

func TestMetadata_Struct(t *testing.T) {
	meta := Metadata{
		Name:    "openai",
		Models:  []string{"gpt-4", "gpt-3.5-turbo"},
		Version: "1.0.0",
	}

	if meta.Name != "openai" {
		t.Errorf("Name = %v, want openai", meta.Name)
	}
	if len(meta.Models) != 2 {
		t.Errorf("len(Models) = %v, want 2", len(meta.Models))
	}
	if meta.Version != "1.0.0" {
		t.Errorf("Version = %v, want 1.0.0", meta.Version)
	}
}

func TestStopReason_Constants(t *testing.T) {
	reasons := []StopReason{
		StopReasonUnknown,
		StopReasonComplete,
		StopReasonToolUse,
		StopReasonMaxTurns,
		StopReasonAborted,
		StopReasonError,
	}
	expected := []string{"unknown", "complete", "tool_use", "max_turns", "aborted", "error"}

	for i, reason := range reasons {
		if string(reason) != expected[i] {
			t.Errorf("StopReason %d = %v, want %v", i, reason, expected[i])
		}
	}
}

func TestEventKind_Constants(t *testing.T) {
	kinds := []EventKind{
		EventTextDelta,
		EventThinkingDelta,
		EventToolCallDelta,
		EventToolCall,
		EventDone,
		EventError,
	}
	expected := []string{
		"text_delta",
		"thinking_delta",
		"tool_call_delta",
		"tool_call",
		"done",
		"error",
	}

	for i, kind := range kinds {
		if string(kind) != expected[i] {
			t.Errorf("EventKind %d = %v, want %v", i, kind, expected[i])
		}
	}
}

func TestToolCallDelta_Struct(t *testing.T) {
	delta := ToolCallDelta{
		ID:             "call-1",
		Name:           "search",
		ArgumentsDelta: "{\"query\":\"test\"}",
	}

	if delta.ID != "call-1" {
		t.Errorf("ID = %v, want call-1", delta.ID)
	}
	if delta.Name != "search" {
		t.Errorf("Name = %v, want search", delta.Name)
	}
	if delta.ArgumentsDelta != "{\"query\":\"test\"}" {
		t.Errorf("ArgumentsDelta = %v, want {\"query\":\"test\"}", delta.ArgumentsDelta)
	}
}

// MockDriver is a test implementation of Driver

type MockDriver struct {
	metadata Metadata
	events   []Event
	err      error
}

func (m *MockDriver) Metadata() Metadata {
	return m.metadata
}

func (m *MockDriver) Stream(ctx context.Context, request Request) (Stream, error) {
	if m.err != nil {
		return nil, m.err
	}
	return NewSliceStream(m.events), nil
}

func TestDriver_Interface(t *testing.T) {
	driver := &MockDriver{
		metadata: Metadata{Name: "test"},
		events:   []Event{{Kind: EventDone}},
	}

	if driver.Metadata().Name != "test" {
		t.Errorf("Metadata().Name = %v, want test", driver.Metadata().Name)
	}

	stream, err := driver.Stream(context.Background(), Request{})
	if err != nil {
		t.Errorf("Stream() error = %v", err)
	}

	event, err := stream.Recv()
	if err != nil {
		t.Errorf("Recv() error = %v", err)
	}
	if event.Kind != EventDone {
		t.Errorf("Event.Kind = %v, want %v", event.Kind, EventDone)
	}
}