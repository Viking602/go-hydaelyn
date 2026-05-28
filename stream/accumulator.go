package stream

import (
	"context"
	"sync"

	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
)

// Accumulator is a Sink that folds a live Frame stream back into the same
// final shape the durable loop persists. It reuses
// provider.NormalizeEvents so accumulation stays byte-for-byte consistent
// with the non-streaming path. Tee an Accumulator alongside a UI sink via
// Broadcast when a consumer needs both the live stream and the final
// message.
//
// Accumulator is safe for concurrent Emit so it can sit behind Merge.
type Accumulator struct {
	mu     sync.Mutex
	events []provider.Event
	tools  []message.ToolResult
}

// NewAccumulator returns an empty Accumulator.
func NewAccumulator() *Accumulator {
	return &Accumulator{}
}

// Emit implements Sink. Provider-derived frames are folded into the
// normalized response; FrameToolResult frames (loop-level enrichment) are
// collected separately and surfaced via ToolResults.
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

// Response folds the accumulated provider frames into a
// NormalizedResponse, identical to the non-streaming loop's view of the
// turn.
func (a *Accumulator) Response() (provider.NormalizedResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return provider.NormalizeEvents(a.events)
}

// Message folds the accumulated frames into a single assistant Message.
func (a *Accumulator) Message() (message.Message, error) {
	response, err := a.Response()
	if err != nil {
		return message.Message{}, err
	}
	return message.Message{
		Role:      message.RoleAssistant,
		Kind:      message.KindStandard,
		Text:      response.Text,
		Thinking:  response.Thinking,
		ToolCalls: response.ToolCalls,
	}, nil
}

// ToolResults returns the tool results observed on the stream in arrival
// order.
func (a *Accumulator) ToolResults() []message.ToolResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]message.ToolResult, len(a.tools))
	copy(out, a.tools)
	return out
}
