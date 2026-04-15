package toolkit

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Viking602/go-hydaelyn/tool"
	mcpclient "github.com/Viking602/go-hydaelyn/transport/mcp/client"
)

func ImportMCPTools(ctx context.Context, client *mcpclient.Client) ([]tool.Driver, error) {
	tools, err := client.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	drivers := make([]tool.Driver, 0, len(tools))
	for _, definition := range tools {
		drivers = append(drivers, remoteTool{
			client:     client,
			definition: tool.Definition(definition),
		})
	}
	return drivers, nil
}

type remoteTool struct {
	client     *mcpclient.Client
	definition tool.Definition
}

func (r remoteTool) Definition() tool.Definition {
	return r.definition
}

func (r remoteTool) Execute(ctx context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	arguments := map[string]any{}
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
			return tool.Result{}, err
		}
	}
	result, err := r.client.CallTool(ctx, call.Name, arguments)
	if err != nil {
		return tool.Result{}, err
	}
	texts := make([]string, 0, len(result.Content))
	for _, block := range result.Content {
		if block.Text != "" {
			texts = append(texts, block.Text)
		}
	}
	structured, _ := json.Marshal(result.StructuredContent)
	return tool.Result{
		ToolCallID: call.ID,
		Name:       call.Name,
		Content:    strings.Join(texts, "\n"),
		Structured: structured,
		IsError:    result.IsError,
	}, nil
}
