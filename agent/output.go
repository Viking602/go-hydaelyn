package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/tool"
)

// FrameKind classifies one transient Engine output frame.
type FrameKind string

const (
	FrameText          FrameKind = "text"
	FrameThinking      FrameKind = "thinking"
	FrameToolCall      FrameKind = "tool_call"
	FrameToolCallDelta FrameKind = "tool_call_delta"
	FrameToolUpdate    FrameKind = "tool_update"
	FrameToolResult    FrameKind = "tool_result"
	FrameDone          FrameKind = "done"
	FrameError         FrameKind = "error"
)

// Frame is one transient unit of Engine output. Source is set by fan-in hosts.
// ToolUpdate frames are live side-channel data and never enter Agent history.
type Frame struct {
	Source           string                  `json:"source,omitempty"`
	Kind             FrameKind               `json:"kind"`
	Text             string                  `json:"text,omitempty"`
	TextPhase        provider.TextPhase      `json:"textPhase,omitempty"`
	Thinking         string                  `json:"thinking,omitempty"`
	Signature        string                  `json:"signature,omitempty"`
	RedactedThinking string                  `json:"redactedThinking,omitempty"`
	ToolCall         *message.ToolCall       `json:"toolCall,omitempty"`
	ToolCallDelta    *provider.ToolCallDelta `json:"toolCallDelta,omitempty"`
	ToolResult       *message.ToolResult     `json:"toolResult,omitempty"`
	ToolUpdate       *tool.Update            `json:"toolUpdate,omitempty"`
	Usage            provider.Usage          `json:"usage,omitempty"`
	StopReason       provider.StopReason     `json:"stopReason,omitempty"`
	ProviderState    json.RawMessage         `json:"providerState,omitempty"`
	Err              error                   `json:"-"`
}

// FrameFromEvent maps a provider event to its consumer-facing frame.
func FrameFromEvent(event provider.Event) (Frame, bool) {
	switch event.Kind {
	case provider.EventTextDelta:
		return Frame{Kind: FrameText, Text: event.Text, TextPhase: event.TextPhase, Usage: event.Usage}, true
	case provider.EventThinkingDelta:
		return Frame{Kind: FrameThinking, Thinking: event.Thinking, Signature: event.Signature, RedactedThinking: event.RedactedThinking, Usage: event.Usage}, true
	case provider.EventToolCall:
		return Frame{Kind: FrameToolCall, ToolCall: event.ToolCall, Usage: event.Usage}, true
	case provider.EventToolCallDelta:
		return Frame{Kind: FrameToolCallDelta, ToolCallDelta: event.ToolCallDelta, Usage: event.Usage}, true
	case provider.EventDone:
		return Frame{Kind: FrameDone, StopReason: event.StopReason, Usage: event.Usage, ProviderState: event.ProviderState}, true
	case provider.EventError:
		return Frame{Kind: FrameError, Err: event.Err, Usage: event.Usage}, true
	default:
		return Frame{}, false
	}
}

// FrameFromToolUpdate returns a transient tool-update frame with no mutable
// data shared with the driver or caller.
func FrameFromToolUpdate(update tool.Update) Frame {
	cloned := tool.CloneUpdate(update)
	return Frame{Kind: FrameToolUpdate, ToolUpdate: &cloned}
}

// ToEvent maps provider-derived frame kinds back to provider events.
func (f Frame) ToEvent() (provider.Event, bool) {
	switch f.Kind {
	case FrameText:
		return provider.Event{Kind: provider.EventTextDelta, Text: f.Text, TextPhase: f.TextPhase, Usage: f.Usage}, true
	case FrameThinking:
		return provider.Event{Kind: provider.EventThinkingDelta, Thinking: f.Thinking, Signature: f.Signature, RedactedThinking: f.RedactedThinking, Usage: f.Usage}, true
	case FrameToolCall:
		return provider.Event{Kind: provider.EventToolCall, ToolCall: f.ToolCall, Usage: f.Usage}, true
	case FrameToolCallDelta:
		return provider.Event{Kind: provider.EventToolCallDelta, ToolCallDelta: f.ToolCallDelta, Usage: f.Usage}, true
	case FrameDone:
		return provider.Event{Kind: provider.EventDone, StopReason: f.StopReason, Usage: f.Usage, ProviderState: f.ProviderState}, true
	case FrameError:
		return provider.Event{Kind: provider.EventError, Err: f.Err, Usage: f.Usage}, true
	default:
		return provider.Event{}, false
	}
}

// Sink receives transient Engine output.
type Sink interface {
	Emit(context.Context, Frame) error
}

// SinkFunc adapts a function to Sink.
type SinkFunc func(context.Context, Frame) error

// Emit delegates to f. A nil function drops the frame.
func (f SinkFunc) Emit(ctx context.Context, frame Frame) error {
	if f == nil {
		return nil
	}
	return f(ctx, frame)
}

// Broadcast emits to each configured sink and joins delivery errors.
type Broadcast struct {
	sinks []Sink
}

// NewBroadcast drops nil sinks and preserves order.
func NewBroadcast(sinks ...Sink) *Broadcast {
	filtered := make([]Sink, 0, len(sinks))
	for _, sink := range sinks {
		if sink != nil {
			filtered = append(filtered, sink)
		}
	}
	return &Broadcast{sinks: filtered}
}

// Emit implements Sink.
func (b *Broadcast) Emit(ctx context.Context, frame Frame) error {
	if b == nil {
		return nil
	}
	var joined []error
	for _, sink := range b.sinks {
		if err := ctx.Err(); err != nil {
			joined = append(joined, err)
			break
		}
		if err := sink.Emit(ctx, frame); err != nil {
			joined = append(joined, err)
		}
	}
	return errors.Join(joined...)
}

// Accumulator folds transient frames into provider-normalized output.
type Accumulator struct {
	mu     sync.Mutex
	events []provider.Event
	tools  []message.ToolResult
}

// NewAccumulator returns an empty Accumulator.
func NewAccumulator() *Accumulator { return &Accumulator{} }

// Emit implements Sink.
func (a *Accumulator) Emit(_ context.Context, frame Frame) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if frame.Kind == FrameToolResult {
		if frame.ToolResult != nil {
			a.tools = append(a.tools, *frame.ToolResult)
		}
		return nil
	}
	if event, ok := frame.ToEvent(); ok {
		a.events = append(a.events, event)
	}
	return nil
}

// Response folds all complete turns and any current partial turn.
func (a *Accumulator) Response() (provider.NormalizedResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return normalizeAccumulatorEvents(a.events, false)
}

// FinalResponse requires a complete terminal stream.
func (a *Accumulator) FinalResponse() (provider.NormalizedResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return normalizeAccumulatorEvents(a.events, true)
}

// Message returns the accumulated assistant message.
func (a *Accumulator) Message() (message.Message, error) {
	response, err := a.Response()
	if err != nil {
		return message.Message{}, err
	}
	result := message.Message{
		Role:          message.RoleAssistant,
		Kind:          message.KindStandard,
		Content:       message.CloneContent(response.Content),
		ToolCalls:     append([]message.ToolCall(nil), response.ToolCalls...),
		ProviderState: append(json.RawMessage(nil), response.ProviderState...),
		Response:      message.CloneResponseMetadata(response.Response),
	}
	result.SyncLegacyContent()
	return result, nil
}

func normalizeAccumulatorEvents(events []provider.Event, requireFinal bool) (provider.NormalizedResponse, error) {
	var combined provider.NormalizedResponse
	start := 0
	for index, event := range events {
		if event.Kind != provider.EventDone && event.Kind != provider.EventError {
			continue
		}
		response, err := provider.NormalizeEvents(events[start : index+1])
		if err != nil {
			return provider.NormalizedResponse{}, err
		}
		mergeNormalizedResponse(&combined, response)
		start = index + 1
	}
	if start < len(events) {
		var response provider.NormalizedResponse
		var err error
		if requireFinal {
			response, err = provider.NormalizeEvents(events[start:])
		} else {
			response, err = provider.NormalizePartialEvents(events[start:])
		}
		if err != nil {
			return provider.NormalizedResponse{}, err
		}
		mergeNormalizedResponse(&combined, response)
	} else if requireFinal && len(events) == 0 {
		return provider.NormalizeEvents(nil)
	}
	return combined, nil
}

func mergeNormalizedResponse(target *provider.NormalizedResponse, source provider.NormalizedResponse) {
	target.Content = append(target.Content, message.CloneContent(source.Content)...)
	target.Text += source.Text
	target.Thinking += source.Thinking
	target.RedactedThinking += source.RedactedThinking
	target.ToolCalls = append(target.ToolCalls, source.ToolCalls...)
	target.Usage = target.Usage.Add(source.Usage)
	if source.Signature != "" {
		target.Signature = source.Signature
	}
	if source.StopReason != "" {
		target.StopReason = source.StopReason
	}
	if len(source.ProviderState) > 0 {
		target.ProviderState = append(target.ProviderState[:0], source.ProviderState...)
	}
	if source.Response.ID != "" || source.Response.Model != "" || len(source.Response.Headers) > 0 {
		target.Response = message.CloneResponseMetadata(source.Response)
	}
}

// ToolResults returns a positional copy of observed tool results.
func (a *Accumulator) ToolResults() []message.ToolResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]message.ToolResult(nil), a.tools...)
}
