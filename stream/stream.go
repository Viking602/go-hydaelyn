// Package stream is the consumer-facing streaming surface for the agent
// loop. It sits above the provider driver SPI: agent/ maps each
// provider.Event into a Frame and pushes it to a Sink while the durable
// loop keeps accumulating the final message. The live Frame stream is a
// transient side-channel — the durable record of a turn is still the
// final message and usage the loop persists, so replay/recovery is
// unaffected (final-state-only durability).
//
// The package is deliberately layered downward only:
//
//	agent/ -> stream/ -> provider/ -> message/
//
// Nothing in provider/ or message/ imports stream/, so the driver SPI
// stays free of consumer concerns.
package stream

import (
	"encoding/json"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
)

// FrameKind classifies a streaming Frame.
type FrameKind string

const (
	// FrameText carries an incremental assistant text delta.
	FrameText FrameKind = "text"
	// FrameThinking carries an incremental reasoning delta.
	FrameThinking FrameKind = "thinking"
	// FrameToolCall carries a complete tool call surfaced by the provider.
	FrameToolCall FrameKind = "tool_call"
	// FrameToolCallDelta carries a partial tool call (providers that stream
	// tool arguments incrementally). Consumers wanting well-formed tool
	// calls should fold the stream with an Accumulator instead.
	FrameToolCallDelta FrameKind = "tool_call_delta"
	// FrameToolResult carries the result of a tool execution. This is a
	// loop-level enrichment with no provider.Event equivalent.
	FrameToolResult FrameKind = "tool_result"
	// FrameDone marks the end of a provider turn and carries the final
	// StopReason and Usage.
	FrameDone FrameKind = "done"
	// FrameError marks a terminal stream error.
	FrameError FrameKind = "error"
)

// Frame is one unit of a live stream. It mirrors provider.Event but is a
// distinct consumer type: it never appears in the driver SPI, so the SPI
// can evolve independently, and it carries a Source label that fan-in
// (Merge) populates so a single consumer can tell which agent produced a
// frame.
type Frame struct {
	// Source is an optional origin label (e.g. an AgentInstance ID). It is
	// empty for single-agent streams and set by Merge for fan-in.
	Source    string             `json:"source,omitempty"`
	Kind      FrameKind          `json:"kind"`
	Text      string             `json:"text,omitempty"`
	TextPhase provider.TextPhase `json:"textPhase,omitempty"`
	Thinking  string             `json:"thinking,omitempty"`
	// Signature carries the opaque thinking-block signature (Anthropic
	// signature_delta) associated with the Thinking delta. Threaded through
	// FrameFromEvent/ToEvent so the streaming path preserves it; without it
	// the next assistant turn's thinking block is rejected by the API.
	Signature string `json:"signature,omitempty"`
	// RedactedThinking carries the opaque payload of a redacted_thinking
	// block delivered whole by the provider. Like Signature, it must
	// round-trip verbatim on the next request when extended thinking is
	// combined with tool use.
	RedactedThinking string                  `json:"redactedThinking,omitempty"`
	ToolCall         *message.ToolCall       `json:"toolCall,omitempty"`
	ToolCallDelta    *provider.ToolCallDelta `json:"toolCallDelta,omitempty"`
	ToolResult       *message.ToolResult     `json:"toolResult,omitempty"`
	Usage            provider.Usage          `json:"usage,omitempty"`
	StopReason       provider.StopReason     `json:"stopReason,omitempty"`
	ProviderState    json.RawMessage         `json:"providerState,omitempty"`
	// Err is set only on FrameError frames. It is not serialized.
	Err error `json:"-"`
}

// FrameFromEvent maps a provider.Event to its faithful Frame. The second
// result is false for an unrecognized event kind, which callers should
// skip rather than forward.
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

// ToEvent is the inverse of FrameFromEvent for the provider-derived
// kinds. FrameToolResult has no provider.Event equivalent and maps to the
// zero Event with ok=false. Accumulator uses this to reuse
// provider.NormalizeEvents.
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
