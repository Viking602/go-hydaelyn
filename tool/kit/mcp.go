package kit

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"github.com/Viking602/venat/tool"
	"github.com/Viking602/venat/transport/mcpcontract"
)

// ErrInvalidMCPClient is returned when an MCP client is nil or otherwise unusable.
var ErrInvalidMCPClient = errors.New("mcp client is invalid")

type MCPClient interface {
	ListTools(ctx context.Context) ([]tool.Definition, error)
	CallTool(ctx context.Context, name string, arguments map[string]any) (mcpcontract.CallToolResult, error)
}

func ImportMCPTools(ctx context.Context, client MCPClient) ([]tool.Driver, error) {
	if isNilMCPClient(client) {
		return nil, ErrInvalidMCPClient
	}
	tools, err := client.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	drivers := make([]tool.Driver, 0, len(tools))
	for _, definition := range tools {
		drivers = append(drivers, remoteTool{
			client:     client,
			definition: definition,
		})
	}
	return drivers, nil
}

func isNilMCPClient(client MCPClient) bool {
	if client == nil {
		return true
	}
	value := reflect.ValueOf(client)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

type remoteTool struct {
	client     MCPClient
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
