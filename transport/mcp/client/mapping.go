package mcpclient

import (
	"encoding/json"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Viking602/go-hydaelyn/message"
)

func mapInitializeResult(result *sdkmcp.InitializeResult) (InitializeResult, error) {
	if result == nil {
		return InitializeResult{}, fmt.Errorf("mcp initialize result is nil")
	}
	return convertJSON[InitializeResult](result)
}

func mapTools(tools []*sdkmcp.Tool) ([]message.ToolDefinition, error) {
	mapped := make([]message.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		item, err := convertJSON[message.ToolDefinition](tool)
		if err != nil {
			return nil, fmt.Errorf("map MCP tool: %w", err)
		}
		mapped = append(mapped, item)
	}
	return mapped, nil
}

func mapResources(resources []*sdkmcp.Resource) ([]Resource, error) {
	mapped := make([]Resource, 0, len(resources))
	for _, resource := range resources {
		item, err := convertJSON[Resource](resource)
		if err != nil {
			return nil, fmt.Errorf("map MCP resource: %w", err)
		}
		mapped = append(mapped, item)
	}
	return mapped, nil
}

func mapResourceContents(contents []*sdkmcp.ResourceContents) ([]ResourceContent, error) {
	mapped := make([]ResourceContent, 0, len(contents))
	for _, content := range contents {
		item, err := convertJSON[ResourceContent](content)
		if err != nil {
			return nil, fmt.Errorf("map MCP resource content: %w", err)
		}
		mapped = append(mapped, item)
	}
	return mapped, nil
}

func mapPrompts(prompts []*sdkmcp.Prompt) ([]Prompt, error) {
	mapped := make([]Prompt, 0, len(prompts))
	for _, prompt := range prompts {
		item, err := convertJSON[Prompt](prompt)
		if err != nil {
			return nil, fmt.Errorf("map MCP prompt: %w", err)
		}
		mapped = append(mapped, item)
	}
	return mapped, nil
}

func mapPromptMessages(messages []*sdkmcp.PromptMessage) ([]PromptMessage, error) {
	mapped := make([]PromptMessage, 0, len(messages))
	for _, promptMessage := range messages {
		item, err := convertJSON[PromptMessage](promptMessage)
		if err != nil {
			return nil, fmt.Errorf("map MCP prompt message: %w", err)
		}
		mapped = append(mapped, item)
	}
	return mapped, nil
}

func convertJSON[T any](value any) (T, error) {
	var result T
	payload, err := json.Marshal(value)
	if err != nil {
		return result, fmt.Errorf("marshal protocol value: %w", err)
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return result, fmt.Errorf("unmarshal client value: %w", err)
	}
	return result, nil
}
