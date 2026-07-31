package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/Viking602/venat/message"
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

// TextPhase identifies the semantic phase of streamed assistant text.
type TextPhase string

const (
	// TextPhaseCommentary is intermediate commentary emitted before the answer.
	TextPhaseCommentary TextPhase = "commentary"
	// TextPhaseFinalAnswer is assistant text intended as the final answer.
	TextPhaseFinalAnswer TextPhase = "final_answer"
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
	InputTokens       int `json:"inputTokens,omitempty"`
	CachedInputTokens int `json:"cachedInputTokens,omitempty"`
	OutputTokens      int `json:"outputTokens,omitempty"`
	TotalTokens       int `json:"totalTokens,omitempty"`
}

func (u Usage) Add(v Usage) Usage {
	return Usage{
		InputTokens:       u.InputTokens + v.InputTokens,
		CachedInputTokens: u.CachedInputTokens + v.CachedInputTokens,
		OutputTokens:      u.OutputTokens + v.OutputTokens,
		TotalTokens:       u.TotalTokens + v.TotalTokens,
	}
}

type ToolCallDelta struct {
	Index          *int   `json:"index,omitempty"`
	ID             string `json:"id,omitempty"`
	Name           string `json:"name,omitempty"`
	ArgumentsDelta string `json:"argumentsDelta,omitempty"`
}

type Request struct {
	Model          string                   `json:"model"`
	Messages       []message.Message        `json:"messages"`
	Tools          []message.ToolDefinition `json:"tools,omitempty"`
	Metadata       map[string]string        `json:"metadata,omitempty"`
	StopSequences  []string                 `json:"stopSequences,omitempty"`
	ThinkingBudget int                      `json:"thinkingBudget,omitempty"`
	ResponseFormat *ResponseFormat          `json:"responseFormat,omitempty"`
	ExtraBody      map[string]any           `json:"extraBody,omitempty"`
}

type ResponseFormat struct {
	Type   string              `json:"type"`
	Name   string              `json:"name,omitempty"`
	Strict bool                `json:"strict,omitempty"`
	Schema *message.JSONSchema `json:"schema,omitempty"`
}

type Event struct {
	Kind      EventKind `json:"kind"`
	Text      string    `json:"text,omitempty"`
	TextPhase TextPhase `json:"textPhase,omitempty"`
	Thinking  string    `json:"thinking,omitempty"`
	// Signature carries the opaque thinking-block signature emitted alongside
	// reasoning (Anthropic signature_delta). It is associated with the
	// current thinking block and accumulated by NormalizeEvents.
	Signature string `json:"signature,omitempty"`
	// RedactedThinking carries the opaque payload of a redacted_thinking
	// block delivered whole by the provider.
	RedactedThinking string            `json:"redactedThinking,omitempty"`
	ToolCall         *message.ToolCall `json:"toolCall,omitempty"`
	ToolCallDelta    *ToolCallDelta    `json:"toolCallDelta,omitempty"`
	Usage            Usage             `json:"usage,omitempty"`
	StopReason       StopReason        `json:"stopReason,omitempty"`
	// ProviderState carries an opaque provider-owned turn payload that must be
	// replayed verbatim on a later request.
	ProviderState json.RawMessage `json:"providerState,omitempty"`
	Err           error           `json:"-"`
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
