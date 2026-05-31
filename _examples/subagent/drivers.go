package main

import (
	"context"
	"encoding/json"

	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
)

// turnDriver is a scripted, turn-aware provider.Driver for the example: it
// returns the next turn's events on each Stream call so a parent can run a
// multi-turn loop (call a tool, then finalize) deterministically without a live
// model. The shipped scripted provider repeats one event list on every call,
// which would loop a tool-calling parent forever; this advances instead.
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
	index := d.call
	if index >= len(d.turns) {
		index = len(d.turns) - 1 // repeat the final turn defensively
	}
	d.call++
	return provider.NewSliceStream(cloneEvents(d.turns[index])), nil
}

func cloneEvents(events []provider.Event) []provider.Event {
	out := make([]provider.Event, len(events))
	copy(out, events)
	return out
}

// newOrchestratorDriver scripts the parent: delegate to summarize, then to
// critique, then write the final answer.
func newOrchestratorDriver() *turnDriver {
	return &turnDriver{
		name:   "anthropic",
		models: []string{modelOrchestrator},
		turns: [][]provider.Event{
			{
				{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{
					ID: "call-summarize", Name: "summarize",
					Arguments: json.RawMessage(`{"input":"Summarize the agent-runtime report."}`),
				}},
				{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
			},
			{
				{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{
					ID: "call-critique", Name: "critique",
					Arguments: json.RawMessage(`{"input":"Critique the three-bullet summary."}`),
				}},
				{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
			},
			{
				{Kind: provider.EventTextDelta, Text: "Reviewed report: summary accepted with the critic's caveats folded in."},
				{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
			},
		},
	}
}

// newSummarizerDriver scripts the fast summarizer subagent (single turn).
func newSummarizerDriver() *turnDriver {
	return &turnDriver{
		name:   "openai",
		models: []string{modelSummarizer},
		turns: [][]provider.Event{{
			{Kind: provider.EventTextDelta, Text: "SUMMARY: durable execution; typed handoffs; explicit scheduling."},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}},
	}
}

// newCriticDriver scripts the deep-reasoning critic subagent (single turn).
func newCriticDriver() *turnDriver {
	return &turnDriver{
		name:   "google",
		models: []string{modelCritic},
		turns: [][]provider.Event{{
			{Kind: provider.EventTextDelta, Text: "CRITIQUE: omits licensing and benchmark methodology."},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}},
	}
}
