package multiagent

import (
	"encoding/json"

	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/api"
)

// Dispatch describes one scheduler decision: assign a Task to an
// AgentInstance with the supplied input and output expectations. Skip
// lets a Scheduler emit a placeholder dispatch (e.g. for DAGScheduler's
// diamond branches) without scheduling actual work.
type Dispatch struct {
	To           string             `json:"to"`
	Task         api.Task           `json:"task"`
	Input        json.RawMessage    `json:"input,omitempty"`
	OutputPolicy agent.OutputPolicy `json:"outputPolicy,omitempty"`
	Skip         bool               `json:"skip,omitempty"`
}
