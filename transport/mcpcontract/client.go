package mcpcontract

import (
	"context"

	"github.com/Viking602/venat/message"
)

type Client interface {
	Initialize(ctx context.Context, name, version string) (InitializeResult, error)
	ListTools(ctx context.Context) ([]message.ToolDefinition, error)
	CallTool(ctx context.Context, name string, arguments map[string]any) (CallToolResult, error)
	ListResources(ctx context.Context) ([]Resource, error)
	ReadResource(ctx context.Context, uri string) ([]ResourceContent, error)
	ListPrompts(ctx context.Context) ([]Prompt, error)
	GetPrompt(ctx context.Context, name string, arguments map[string]string) ([]PromptMessage, error)
	Close() error
}

type Elicitation struct {
	Mode            string
	Message         string
	URL             string
	ElicitationID   string
	RequestedSchema any
}

type ElicitationResult struct {
	Action  string
	Content map[string]any
}

type ElicitationHandler func(context.Context, Elicitation) (ElicitationResult, error)

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InitializeResult struct {
	ProtocolVersion string         `json:"protocolVersion,omitempty"`
	ServerInfo      ServerInfo     `json:"serverInfo,omitempty"`
	Capabilities    map[string]any `json:"capabilities,omitempty"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type CallToolResult struct {
	Content           []ContentBlock `json:"content"`
	IsError           bool           `json:"isError,omitempty"`
	StructuredContent map[string]any `json:"structuredContent,omitempty"`
}

type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

type Prompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type PromptMessage struct {
	Role    string       `json:"role"`
	Content ContentBlock `json:"content"`
}
