package provider

import (
	"context"
	"errors"
	"io"

	"github.com/Viking602/go-hydaelyn/message"
)

type StopReason string

const (
	StopReasonUnknown  StopReason = "unknown"
	StopReasonComplete StopReason = "complete"
	StopReasonToolUse  StopReason = "tool_use"
	StopReasonMaxTurns StopReason = "max_turns"
	StopReasonAborted  StopReason = "aborted"
	StopReasonError    StopReason = "error"
)

type EventKind string

const (
	EventTextDelta     EventKind = "text_delta"
	EventThinkingDelta EventKind = "thinking_delta"
	EventToolCallDelta EventKind = "tool_call_delta"
	EventToolCall      EventKind = "tool_call"
	EventDone          EventKind = "done"
	EventError         EventKind = "error"
)

type Metadata struct {
	Name    string   `json:"name"`
	Models  []string `json:"models,omitempty"`
	Version string   `json:"version,omitempty"`
}

type Usage struct {
	InputTokens  int `json:"inputTokens,omitempty"`
	OutputTokens int `json:"outputTokens,omitempty"`
	TotalTokens  int `json:"totalTokens,omitempty"`
}

type ToolCallDelta struct {
	ID             string `json:"id,omitempty"`
	Name           string `json:"name,omitempty"`
	ArgumentsDelta string `json:"argumentsDelta,omitempty"`
}

type Request struct {
	Model    string                   `json:"model"`
	Messages []message.Message        `json:"messages"`
	Tools    []message.ToolDefinition `json:"tools,omitempty"`
	Metadata map[string]string        `json:"metadata,omitempty"`
}

type Event struct {
	Kind          EventKind         `json:"kind"`
	Text          string            `json:"text,omitempty"`
	Thinking      string            `json:"thinking,omitempty"`
	ToolCall      *message.ToolCall `json:"toolCall,omitempty"`
	ToolCallDelta *ToolCallDelta    `json:"toolCallDelta,omitempty"`
	Usage         Usage             `json:"usage,omitempty"`
	StopReason    StopReason        `json:"stopReason,omitempty"`
	Err           error             `json:"-"`
}

type Stream interface {
	Recv() (Event, error)
	Close() error
}

type Driver interface {
	Metadata() Metadata
	Stream(ctx context.Context, request Request) (Stream, error)
}

var ErrNotImplemented = errors.New("provider driver not implemented")

type SliceStream struct {
	events []Event
	index  int
}

func NewSliceStream(events []Event) *SliceStream {
	return &SliceStream{events: events}
}

func (s *SliceStream) Recv() (Event, error) {
	if s.index >= len(s.events) {
		return Event{}, io.EOF
	}
	event := s.events[s.index]
	s.index++
	return event, nil
}

func (s *SliceStream) Close() error {
	return nil
}
