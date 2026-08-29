package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/tool"
	"github.com/Viking602/venat/tool/kit"
)

type demoProvider struct{}

func (demoProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "example"}
}

func (demoProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	if len(request.Messages) > 0 && request.Messages[len(request.Messages)-1].Role == message.RoleTool {
		return provider.NewSliceStream([]provider.Event{
			{Kind: provider.EventTextDelta, Text: "lookup complete"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}), nil
	}
	return provider.NewSliceStream([]provider.Event{
		{
			Kind: provider.EventToolCall,
			ToolCall: &message.ToolCall{
				ID:        "lookup-1",
				Name:      "lookup",
				Arguments: json.RawMessage(`{"query":"venat"}`),
			},
		},
		{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
	}), nil
}

func main() {
	calls := 0
	lookup, err := kit.Tool("lookup", func(_ context.Context, input struct {
		Query string `json:"query"`
	}, sink tool.UpdateSink,
	) (string, error) {
		calls++
		result := "found:" + input.Query
		if err := sink(tool.Update{Kind: tool.UpdateProgress, Message: "searching"}); err != nil {
			return "", err
		}
		if err := sink(tool.Update{Kind: tool.UpdateOutput, Parts: []message.ContentPart{message.TextPart(result)}}); err != nil {
			return "", err
		}
		return result, nil
	})
	if err != nil {
		panic(err)
	}

	engine := agent.Engine{
		Provider: demoProvider{},
		Tools:    tool.NewBus(lookup),
		Model:    "example-model",
		ToolMode: tool.ModeSequential,
	}
	updates := 0
	result := engine.RunStream(
		context.Background(),
		agent.Request{Prompt: "Find Venat"},
		agent.OutputPolicy{},
		agent.SinkFunc(func(_ context.Context, frame agent.Frame) error {
			if frame.Kind == agent.FrameToolUpdate {
				updates++
			}
			return nil
		}),
	)
	if result.Failure != nil {
		panic(result.Failure)
	}
	fmt.Printf("agent: model->tool updates->output; calls=%d; updates=%d; text=%q\n", calls, updates, result.Text)
}
