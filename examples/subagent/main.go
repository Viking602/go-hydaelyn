package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/tool"
)

type childProvider struct{}

func (childProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "child-example"}
}

func (childProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	if len(request.Messages) == 0 || request.Messages[len(request.Messages)-1].Text != "research" {
		return nil, fmt.Errorf("child received unexpected prompt")
	}
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "child answer", TextPhase: provider.TextPhaseFinalAnswer},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

type parentProvider struct{}

func (parentProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "parent-example"}
}

func (parentProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	if len(request.Messages) > 0 {
		last := request.Messages[len(request.Messages)-1]
		if last.Role == message.RoleTool && last.ToolResult != nil {
			return provider.NewSliceStream([]provider.Event{
				{Kind: provider.EventTextDelta, Text: "parent received: " + last.ToolResult.Content},
				{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
			}), nil
		}
	}
	return provider.NewSliceStream([]provider.Event{
		{
			Kind: provider.EventToolCall,
			ToolCall: &message.ToolCall{
				ID:        "research-1",
				Name:      "researcher",
				Arguments: json.RawMessage(`{"task":"research"}`),
			},
		},
		{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
	}), nil
}

func main() {
	child := agent.Engine{
		Provider: childProvider{},
		Model:    "child-model",
	}
	researcher, err := agent.NewAgentTool(child, agent.AgentToolConfig{
		Definition: tool.Definition{
			Name:             "researcher",
			Description:      "Delegate a research task to the child agent.",
			ConcurrencyGroup: "subagents",
			MaxConcurrency:   4,
		},
	})
	if err != nil {
		panic(err)
	}

	tools := tool.NewBus(researcher)
	if err := tools.Validate(); err != nil {
		panic(err)
	}
	parent := agent.Engine{
		Provider: parentProvider{},
		Tools:    tools,
		Model:    "parent-model",
		ToolMode: tool.ModeParallel,
	}
	progress := 0
	result := parent.RunStream(
		context.Background(),
		agent.Request{Prompt: "Delegate the research task."},
		agent.OutputPolicy{},
		agent.SinkFunc(func(_ context.Context, frame agent.Frame) error {
			if frame.Kind == agent.FrameToolUpdate {
				progress++
			}
			return nil
		}),
	)
	if result.Failure != nil {
		panic(result.Failure)
	}
	fmt.Printf("subagent: progress=%d; text=%q\n", progress, result.Text)
}
