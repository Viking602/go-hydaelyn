package message

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewText(t *testing.T) {
	tests := []struct {
		name     string
		role     Role
		text     string
		wantRole Role
		wantText string
		wantKind Kind
	}{
		{
			name:     "system message",
			role:     RoleSystem,
			text:     "You are a helpful assistant",
			wantRole: RoleSystem,
			wantText: "You are a helpful assistant",
			wantKind: KindStandard,
		},
		{
			name:     "user message",
			role:     RoleUser,
			text:     "Hello",
			wantRole: RoleUser,
			wantText: "Hello",
			wantKind: KindStandard,
		},
		{
			name:     "assistant message",
			role:     RoleAssistant,
			text:     "Hi there",
			wantRole: RoleAssistant,
			wantText: "Hi there",
			wantKind: KindStandard,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := NewText(tt.role, tt.text)

			if msg.Role != tt.wantRole {
				t.Errorf("Role = %v, want %v", msg.Role, tt.wantRole)
			}
			if msg.Text != tt.wantText {
				t.Errorf("Text = %v, want %v", msg.Text, tt.wantText)
			}
			if msg.Kind != tt.wantKind {
				t.Errorf("Kind = %v, want %v", msg.Kind, tt.wantKind)
			}
			if msg.Visibility != VisibilityShared {
				t.Errorf("Visibility = %v, want %v", msg.Visibility, VisibilityShared)
			}
			if msg.CreatedAt.IsZero() {
				t.Error("CreatedAt should not be zero")
			}
		})
	}
}

func TestNewToolResult(t *testing.T) {
	result := ToolResult{
		ToolCallID: "call-1",
		Name:       "test-tool",
		Content:    "tool output",
		IsError:    false,
	}

	msg := NewToolResult(result)

	if msg.Role != RoleTool {
		t.Errorf("Role = %v, want %v", msg.Role, RoleTool)
	}
	if msg.Kind != KindStandard {
		t.Errorf("Kind = %v, want %v", msg.Kind, KindStandard)
	}
	if msg.Name != "test-tool" {
		t.Errorf("Name = %v, want test-tool", msg.Name)
	}
	if msg.ToolResult == nil {
		t.Fatal("ToolResult should not be nil")
	}
	if msg.ToolResult.Content != "tool output" {
		t.Errorf("ToolResult.Content = %v, want tool output", msg.ToolResult.Content)
	}
	if msg.Visibility != VisibilityShared {
		t.Errorf("Visibility = %v, want %v", msg.Visibility, VisibilityShared)
	}
}

func TestMessage_StructFields(t *testing.T) {
	toolCalls := []ToolCall{
		{ID: "call-1", Name: "tool1", Arguments: json.RawMessage(`{"arg":"value"}`)},
	}

	msg := Message{
		ID:          "msg-1",
		Role:        RoleAssistant,
		Kind:        KindStandard,
		Name:        "assistant",
		Text:        "Hello",
		Thinking:    "thinking process",
		ToolCalls:   toolCalls,
		TeamID:      "team-1",
		AgentID:     "agent-1",
		RunID:       "run-1",
		ParentRunID: "parent-1",
		Visibility:  VisibilityPrivate,
		Metadata:    map[string]string{"key": "value"},
		CreatedAt:   time.Now().UTC(),
	}

	if msg.ID != "msg-1" {
		t.Errorf("ID = %v, want msg-1", msg.ID)
	}
	if msg.Role != RoleAssistant {
		t.Errorf("Role = %v, want %v", msg.Role, RoleAssistant)
	}
	if msg.Name != "assistant" {
		t.Errorf("Name = %v, want assistant", msg.Name)
	}
	if msg.Text != "Hello" {
		t.Errorf("Text = %v, want Hello", msg.Text)
	}
	if msg.Thinking != "thinking process" {
		t.Errorf("Thinking = %v, want thinking process", msg.Thinking)
	}
	if len(msg.ToolCalls) != 1 {
		t.Errorf("len(ToolCalls) = %v, want 1", len(msg.ToolCalls))
	}
	if msg.TeamID != "team-1" {
		t.Errorf("TeamID = %v, want team-1", msg.TeamID)
	}
	if msg.Visibility != VisibilityPrivate {
		t.Errorf("Visibility = %v, want %v", msg.Visibility, VisibilityPrivate)
	}
}

func TestToolDefinition_Struct(t *testing.T) {
	schema := JSONSchema{
		Type:        "object",
		Description: "Test schema",
		Properties: map[string]JSONSchema{
			"name": {Type: "string"},
		},
		Required: []string{"name"},
	}

	def := ToolDefinition{
		Name:        "test-tool",
		Description: "A test tool",
		InputSchema: schema,
		Tags:        []string{"test", "demo"},
		Terminal:    true,
		Metadata:    map[string]string{"version": "1.0"},
		Origin:      "test",
		Security: ToolSecurity{
			RequiredPermissions: []string{"tool:test"},
			RequiresApproval:    true,
			RiskLevel:           "high",
			Idempotent:          false,
		},
	}

	if def.Name != "test-tool" {
		t.Errorf("Name = %v, want test-tool", def.Name)
	}
	if def.Description != "A test tool" {
		t.Errorf("Description = %v, want A test tool", def.Description)
	}
	if def.InputSchema.Type != "object" {
		t.Errorf("InputSchema.Type = %v, want object", def.InputSchema.Type)
	}
	if len(def.Tags) != 2 {
		t.Errorf("len(Tags) = %v, want 2", len(def.Tags))
	}
	if !def.Terminal {
		t.Errorf("Terminal = %v, want true", def.Terminal)
	}
	if len(def.Security.RequiredPermissions) != 1 || def.Security.RequiredPermissions[0] != "tool:test" {
		t.Errorf("unexpected tool security %#v", def.Security)
	}
}

func TestJSONSchema_Struct(t *testing.T) {
	schema := JSONSchema{
		Type:        "object",
		Description: "Root schema",
		Properties: map[string]JSONSchema{
			"count": {
				Type: "integer",
				Enum: []string{"1", "2", "3"},
			},
		},
		Required:             []string{"count"},
		AdditionalProperties: true,
	}

	if schema.Type != "object" {
		t.Errorf("Type = %v, want object", schema.Type)
	}
	if schema.AdditionalProperties != true {
		t.Errorf("AdditionalProperties = %v, want true", schema.AdditionalProperties)
	}
	if len(schema.Properties) != 1 {
		t.Errorf("len(Properties) = %v, want 1", len(schema.Properties))
	}
}

func TestToolCall_Struct(t *testing.T) {
	call := ToolCall{
		ID:        "call-123",
		Name:      "search",
		Arguments: json.RawMessage(`{"query":"test"}`),
	}

	if call.ID != "call-123" {
		t.Errorf("ID = %v, want call-123", call.ID)
	}
	if call.Name != "search" {
		t.Errorf("Name = %v, want search", call.Name)
	}
	if string(call.Arguments) != `{"query":"test"}` {
		t.Errorf("Arguments = %v, want {\"query\":\"test\"}", string(call.Arguments))
	}
}

func TestToolResult_Struct(t *testing.T) {
	result := ToolResult{
		ToolCallID: "call-123",
		Name:       "search",
		Content:    "search results",
		Structured: json.RawMessage(`{"results":[]}`),
		IsError:    true,
	}

	if result.ToolCallID != "call-123" {
		t.Errorf("ToolCallID = %v, want call-123", result.ToolCallID)
	}
	if result.IsError != true {
		t.Errorf("IsError = %v, want true", result.IsError)
	}
}

func TestRole_Constants(t *testing.T) {
	roles := []Role{RoleSystem, RoleUser, RoleAssistant, RoleTool, RoleCustom}
	expected := []string{"system", "user", "assistant", "tool", "custom"}

	for i, role := range roles {
		if string(role) != expected[i] {
			t.Errorf("Role %d = %v, want %v", i, role, expected[i])
		}
	}
}

func TestKind_Constants(t *testing.T) {
	kinds := []Kind{KindStandard, KindBranchSummary, KindCompactionSummary, KindCommandOutput, KindCustom}
	expected := []string{"standard", "branch_summary", "compaction_summary", "command_output", "custom"}

	for i, kind := range kinds {
		if string(kind) != expected[i] {
			t.Errorf("Kind %d = %v, want %v", i, kind, expected[i])
		}
	}
}

func TestVisibility_Constants(t *testing.T) {
	visibilities := []Visibility{VisibilityShared, VisibilityPrivate}
	expected := []string{"shared", "private"}

	for i, vis := range visibilities {
		if string(vis) != expected[i] {
			t.Errorf("Visibility %d = %v, want %v", i, vis, expected[i])
		}
	}
}
