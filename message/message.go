package message

import (
	"encoding/json"
	"time"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
	RoleCustom    Role = "custom"
)

type Kind string

const (
	KindStandard          Kind = "standard"
	KindBranchSummary     Kind = "branch_summary"
	KindCompactionSummary Kind = "compaction_summary"
	KindCommandOutput     Kind = "command_output"
	KindCustom            Kind = "custom"
)

type Visibility string

const (
	VisibilityShared  Visibility = "shared"
	VisibilityPrivate Visibility = "private"
)

type JSONSchema struct {
	Type                 string                `json:"type,omitempty"`
	Description          string                `json:"description,omitempty"`
	Properties           map[string]JSONSchema `json:"properties,omitempty"`
	Required             []string              `json:"required,omitempty"`
	Items                *JSONSchema           `json:"items,omitempty"`
	Enum                 []string              `json:"enum,omitempty"`
	AdditionalProperties bool                  `json:"additionalProperties,omitempty"`
}

type ToolSecurity struct {
	RequiredPermissions []string `json:"requiredPermissions,omitempty"`
	RequiresApproval    bool     `json:"requiresApproval,omitempty"`
	RiskLevel           string   `json:"riskLevel,omitempty"`
	Idempotent          bool     `json:"idempotent,omitempty"`
}

type ToolDefinition struct {
	Name                string            `json:"name"`
	Description         string            `json:"description,omitempty"`
	InputSchema         JSONSchema        `json:"inputSchema"`
	Terminal            bool              `json:"terminal,omitempty"`
	Tags                []string          `json:"tags,omitempty"`
	Metadata            map[string]string `json:"metadata,omitempty"`
	Origin              string            `json:"origin,omitempty"`
	Security            ToolSecurity      `json:"security,omitempty"`
	RequiredPermissions []string          `json:"requiredPermissions,omitempty"`
	RequiresApproval    bool              `json:"requiresApproval,omitempty"`
	RiskLevel           string            `json:"riskLevel,omitempty"`
}

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type ToolResult struct {
	ToolCallID string          `json:"toolCallId,omitempty"`
	Name       string          `json:"name"`
	Content    string          `json:"content,omitempty"`
	Structured json.RawMessage `json:"structured,omitempty"`
	IsError    bool            `json:"isError,omitempty"`
}

type Message struct {
	ID          string            `json:"id,omitempty"`
	Role        Role              `json:"role"`
	Kind        Kind              `json:"kind,omitempty"`
	Name        string            `json:"name,omitempty"`
	Text        string            `json:"text,omitempty"`
	Thinking    string            `json:"thinking,omitempty"`
	ToolCalls   []ToolCall        `json:"toolCalls,omitempty"`
	ToolResult  *ToolResult       `json:"toolResult,omitempty"`
	TeamID      string            `json:"teamId,omitempty"`
	AgentID     string            `json:"agentId,omitempty"`
	RunID       string            `json:"runId,omitempty"`
	ParentRunID string            `json:"parentRunId,omitempty"`
	Visibility  Visibility        `json:"visibility,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	CreatedAt   time.Time         `json:"createdAt,omitempty"`
}

func NewText(role Role, text string) Message {
	return Message{
		Role:       role,
		Kind:       KindStandard,
		Text:       text,
		Visibility: VisibilityShared,
		CreatedAt:  time.Now().UTC(),
	}
}

func NewToolResult(result ToolResult) Message {
	return Message{
		Role:       RoleTool,
		Kind:       KindStandard,
		Name:       result.Name,
		ToolResult: &result,
		Visibility: VisibilityShared,
		CreatedAt:  time.Now().UTC(),
	}
}
