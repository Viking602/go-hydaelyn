package agent

import (
	"encoding/json"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
)

// Result is the typed outcome of Engine.Run on one api.Task. The
// multi-agent scheduler branches on Failure (when set) and consumes
// Structured (when OutputPolicy.Schema was supplied) to decide the next
// Dispatch.
//
// Failure is the only failure shape that crosses the agent → multiagent
// boundary (boundaries doc Principle 6). A bare error return is rejected
// by the v0.8.0 spec; surface failures via Result.Failure.
type Result struct {
	Text        string              `json:"text,omitempty"`
	Structured  json.RawMessage     `json:"structured,omitempty"`
	Valid       bool                `json:"valid,omitempty"`
	RepairCount int                 `json:"repairCount,omitempty"`
	Failure     *AgentFailure       `json:"failure,omitempty"`
	Steps       []Step              `json:"steps,omitempty"`
	Usage       provider.Usage      `json:"usage,omitempty"`
	StopReason  provider.StopReason `json:"stopReason,omitempty"`
	Messages    []message.Message   `json:"messages,omitempty"`
	Thinking    string              `json:"thinking,omitempty"`
}
