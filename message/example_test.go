package message_test

import (
	"encoding/json"
	"fmt"

	"github.com/Viking602/venat/message"
)

// ExampleNewText demonstrates building a user-authored conversation turn.
// CreatedAt is intentionally not part of the printed output because NewText
// stamps it with time.Now() — only the deterministic fields are shown.
func ExampleNewText() {
	msg := message.NewText(message.RoleUser, "What is the weather today?")
	fmt.Printf("role=%s kind=%s visibility=%s text=%q\n",
		msg.Role, msg.Kind, msg.Visibility, msg.Text)
	// Output:
	// role=user kind=standard visibility=shared text="What is the weather today?"
}

// ExampleNewToolResult demonstrates wrapping a tool's structured output into
// a message that the runtime can route back to the calling agent.
func ExampleNewToolResult() {
	structured, _ := json.Marshal(map[string]any{
		"temp_c":    21,
		"condition": "sunny",
	})
	msg := message.NewToolResult(message.ToolResult{
		ToolCallID: "call_abc123",
		Name:       "get_weather",
		Content:    "21°C, sunny",
		Structured: structured,
	})
	fmt.Printf("role=%s name=%s tool_id=%s content=%q\n",
		msg.Role, msg.Name, msg.ToolResult.ToolCallID, msg.ToolResult.Content)
	// Output:
	// role=tool name=get_weather tool_id=call_abc123 content="21°C, sunny"
}
