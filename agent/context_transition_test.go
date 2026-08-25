package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/tool"
)

type checkpointTransitionFixture struct {
	snapshot []message.Message
}

func (fixture *checkpointTransitionFixture) Apply(_ context.Context, history []message.Message, results []tool.Result) ([]message.Message, error) {
	for _, result := range results {
		switch result.Name {
		case "checkpoint":
			fixture.snapshot = message.CloneMessages(history)
		case "rewind":
			return append(message.CloneMessages(fixture.snapshot), message.NewText(message.RoleSystem, "Rewind report: durable finding")), nil
		}
	}
	return history, nil
}

func TestContextTransitionRewindsToolHistoryBeforeNextModelTurn(t *testing.T) {
	fixture := &checkpointTransitionFixture{}
	tools := tool.NewBus(
		contextTransitionDriver{name: "checkpoint"},
		contextTransitionDriver{name: "lookup"},
		contextTransitionDriver{name: "rewind"},
	)
	engine := Engine{Provider: &scriptedProvider{turns: [][]provider.Event{
		{{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{ID: "checkpoint", Name: "checkpoint", Arguments: json.RawMessage(`{}`)}}, {Kind: provider.EventDone, StopReason: provider.StopReasonToolUse}},
		{{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{ID: "lookup", Name: "lookup", Arguments: json.RawMessage(`{}`)}}, {Kind: provider.EventDone, StopReason: provider.StopReasonToolUse}},
		{{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{ID: "rewind", Name: "rewind", Arguments: json.RawMessage(`{}`)}}, {Kind: provider.EventDone, StopReason: provider.StopReasonToolUse}},
		{{Kind: provider.EventTextDelta, Text: "final answer"}, {Kind: provider.EventDone, StopReason: provider.StopReasonComplete}},
	}}, Tools: tools, ContextTransition: fixture}
	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model: "model", Messages: []message.Message{message.NewText(message.RoleUser, "investigate")}, MaxIterations: 4,
		ContextTransition: fixture,
	})
	if err != nil {
		t.Fatal(err)
	}
	var rendered []string
	for _, current := range output.Messages {
		rendered = append(rendered, current.Text)
		for _, call := range current.ToolCalls {
			rendered = append(rendered, call.Name)
		}
		if current.ToolResult != nil {
			rendered = append(rendered, current.ToolResult.Name, current.ToolResult.Content)
		}
	}
	history := strings.Join(rendered, "\n")
	if !strings.Contains(history, "checkpoint") || !strings.Contains(history, "Rewind report: durable finding") || !strings.Contains(history, "final answer") {
		t.Fatalf("rewound history missing retained state: %q", history)
	}
	if strings.Contains(history, "lookup") || strings.Contains(history, "result for rewind") {
		t.Fatalf("rewound history retained exploration: %q", history)
	}
}

type contextTransitionDriver struct{ name string }

func (driver contextTransitionDriver) Definition() tool.Definition {
	return tool.Definition{Name: driver.name, InputSchema: tool.Schema{Type: "object"}}
}

func (driver contextTransitionDriver) Execute(_ context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	return tool.Result{ToolCallID: call.ID, Name: call.Name, Content: "result for " + call.Name}, nil
}
