package agent

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/session"
)

type streamCollect struct {
	text          string
	thinking      string
	signature     string
	redacted      string
	providerState []byte
	usage         session.Usage
	stop          provider.StopReason
	sawTool       bool
	// err is a provider-side failure: it becomes the verdict recorded on the
	// assistant entry. storageErr is a session read that failed before the
	// provider was ever called, so it is never a verdict about the model.
	err        error
	storageErr error
}

// runStream drains one provider turn. It shares Engine's per-turn event and
// byte ceilings (maxProviderTurnEvents / maxProviderTurnBytes): a provider that
// never stops streaming must not grow the assistant entry without bound. Over
// the limit the turn ends as a non-retryable ErrProviderTurnLimit failure,
// keeping whatever was collected before the offending event.
func (h *Harness) runStream(ctx context.Context, leafID, model string) streamCollect {
	var out streamCollect
	msgs, err := h.sess.ContextMessages(ctx, leafID)
	if err != nil {
		out.storageErr = err
		return out
	}
	stream, err := h.opts.Provider.Stream(ctx, provider.Request{Model: model, Messages: msgs})
	if err != nil {
		out.err = err
		return out
	}
	defer stream.Close()

	events := 0
	eventBytes := 0
	for {
		ev, recvErr := stream.Recv()
		if recvErr != nil {
			if !errors.Is(recvErr, io.EOF) {
				out.err = recvErr
			}
			return out
		}
		events++
		if events > maxProviderTurnEvents {
			return out.overLimit(fmt.Sprintf("more than %d events", maxProviderTurnEvents))
		}
		size := providerEventSize(ev)
		if size > maxProviderTurnBytes-eventBytes {
			return out.overLimit(fmt.Sprintf("more than %d bytes", maxProviderTurnBytes))
		}
		eventBytes += size

		out.text += ev.Text
		out.thinking += ev.Thinking
		if ev.Signature != "" {
			out.signature = ev.Signature
		}
		out.redacted += ev.RedactedThinking
		if len(ev.ProviderState) > 0 {
			out.providerState = ev.ProviderState
		}
		if ev.Usage != (provider.Usage{}) {
			out.usage = session.Usage{
				InputTokens:  ev.Usage.InputTokens,
				OutputTokens: ev.Usage.OutputTokens,
				TotalTokens:  ev.Usage.TotalTokens,
			}
		}
		if ev.StopReason != "" {
			out.stop = ev.StopReason
		}
		if ev.Kind == provider.EventToolCall || ev.Kind == provider.EventToolCallDelta || ev.ToolCall != nil || ev.ToolCallDelta != nil {
			out.sawTool = true
		}
		switch ev.Kind {
		case provider.EventDone:
			return out
		case provider.EventError:
			out.err = ev.Err
			if out.err == nil {
				out.err = errors.New("provider event error")
			}
			if out.stop == "" {
				out.stop = provider.StopReasonError
			}
			return out
		}
	}
}

func (c streamCollect) overLimit(reason string) streamCollect {
	c.err = fmt.Errorf("%w: %s", ErrProviderTurnLimit, reason)
	c.stop = provider.StopReasonError
	return c
}
