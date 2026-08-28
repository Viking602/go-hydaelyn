package stream

import (
	"context"
	"sync"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
)

// Accumulator is a Sink that folds a live Frame stream back into the same
// final shape the durable loop persists. It reuses
// provider.NormalizeEvents so accumulation stays byte-for-byte consistent
// with the non-streaming path. Tee an Accumulator alongside a UI sink via
// Broadcast when a consumer needs both the live stream and the final
// message.
//
// Accumulator is safe for concurrent Emit from independently driven streams.
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

// Response folds every provider frame observed so far. It remains available
// before the terminal frame for live UI projections.
func (a *Accumulator) Response() (provider.NormalizedResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return normalizeAccumulatorEvents(a.events, false)
}

// FinalResponse requires a complete, terminal provider stream.
func (a *Accumulator) FinalResponse() (provider.NormalizedResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return normalizeAccumulatorEvents(a.events, true)
}

// Message folds the accumulated frames into a single assistant Message.
func (a *Accumulator) Message() (message.Message, error) {
	response, err := a.Response()
	if err != nil {
		return message.Message{}, err
	}
	result := message.Message{
		Role:      message.RoleAssistant,
		Kind:      message.KindStandard,
		Content:   message.CloneContent(response.Content),
		ToolCalls: response.ToolCalls,
		Response:  message.CloneResponseMetadata(response.Response),
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
		var (
			response provider.NormalizedResponse
			err      error
		)
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

// ToolResults returns the tool results observed on the stream in arrival
// order.
func (a *Accumulator) ToolResults() []message.ToolResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]message.ToolResult, len(a.tools))
	copy(out, a.tools)
	return out
}
