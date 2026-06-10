package main

import (
	"context"
	"encoding/json"

	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
)

// turnDriver is a deterministic, turn-aware provider.Driver: each Stream call
// returns the next scripted turn, so an agent can run a multi-turn loop offline
// without a live model. The shipped scripted provider repeats one turn on every
// call, which would never let a tool-calling loop converge; this advances.
type turnDriver struct {
	name   string
	models []string
	turns  [][]provider.Event
	call   int
}

func (d *turnDriver) Metadata() provider.Metadata {
	return provider.Metadata{Name: d.name, Models: d.models}
}

func (d *turnDriver) Stream(_ context.Context, _ provider.Request) (provider.Stream, error) {
	i := d.call
	if i >= len(d.turns) {
		i = len(d.turns) - 1 // repeat the final turn defensively
	}
	d.call++
	return provider.NewSliceStream(cloneEvents(d.turns[i])), nil
}

func cloneEvents(events []provider.Event) []provider.Event {
	out := make([]provider.Event, len(events))
	copy(out, events)
	return out
}

// say is an assistant text delta — the model "speaking" within a turn.
func say(text string) provider.Event {
	return provider.Event{Kind: provider.EventTextDelta, Text: text}
}

// callVerify is a tool call to the verifier subagent, passing the draft as the
// "input" argument the subagent maps to its task goal.
func callVerify(id, draft string) provider.Event {
	args, _ := json.Marshal(map[string]string{"input": draft})
	return provider.Event{
		Kind:     provider.EventToolCall,
		ToolCall: &message.ToolCall{ID: id, Name: "verify", Arguments: args},
	}
}

// done terminates a turn with the given stop reason.
func done(reason provider.StopReason) provider.Event {
	return provider.Event{Kind: provider.EventDone, StopReason: reason}
}
