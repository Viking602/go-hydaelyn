package agent

import (
	"encoding/json"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
)

// Result is the complete typed outcome of one Engine execution. Failure is
// data; callers inspect its kind and error chain without losing the transcript.
type Result struct {
	Text          string              `json:"text,omitempty"`
	Structured    json.RawMessage     `json:"structured,omitempty"`
	Valid         bool                `json:"valid,omitempty"`
	RepairCount   int                 `json:"repairCount,omitempty"`
	Failure       *AgentFailure       `json:"failure,omitempty"`
	Steps         []Step              `json:"steps,omitempty"`
	Usage         provider.Usage      `json:"usage,omitempty"`
	ToolCallsUsed int                 `json:"toolCallsUsed,omitempty"`
	StopReason    provider.StopReason `json:"stopReason,omitempty"`
	Messages      []message.Message   `json:"messages,omitempty"`
	Thinking      string              `json:"thinking,omitempty"`
}
