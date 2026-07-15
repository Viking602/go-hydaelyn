package multiagent

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/api"
)

// Dispatch describes one scheduler decision: assign a Task to an
// AgentInstance with the supplied input and output expectations. Skip
// lets a Scheduler emit a placeholder dispatch (e.g. for DAGScheduler's
// diamond branches) without scheduling actual work.
type Dispatch struct {
	To             string             `json:"to"`
	ClassName      string             `json:"className,omitempty"`
	AgentClassName string             `json:"agentClassName,omitempty"`
	Task           api.Task           `json:"task"`
	Input          json.RawMessage    `json:"input,omitempty"`
	OutputPolicy   agent.OutputPolicy `json:"outputPolicy,omitempty"`
	Handoff        *Handoff           `json:"handoff,omitempty"`
	Skip           bool               `json:"skip,omitempty"`
}

// ValidateDispatch enforces the typed handoff contract before execution.
func ValidateDispatch(dispatch Dispatch) error {
	handoff := dispatch.Handoff
	if handoff != nil {
		if handoff.RunID != "" && handoff.RunID != dispatch.Task.RunID {
			return fmt.Errorf("multiagent: handoff run %q does not match task run %q", handoff.RunID, dispatch.Task.RunID)
		}
		if handoff.To != "" && handoff.To != dispatch.To {
			return fmt.Errorf("multiagent: handoff target %q does not match dispatch target %q", handoff.To, dispatch.To)
		}
	}
	inputs := []json.RawMessage{dispatch.Task.Input, dispatch.Input}
	if handoff != nil {
		inputs = append(inputs, handoff.Payload)
	}
	var input json.RawMessage
	for _, candidate := range inputs {
		if len(candidate) == 0 {
			continue
		}
		if !json.Valid(candidate) {
			return fmt.Errorf("multiagent: dispatch input is not valid JSON")
		}
		if len(input) == 0 {
			input = candidate
			continue
		}
		if !jsonEqual(input, candidate) {
			return fmt.Errorf("multiagent: task, dispatch, and handoff inputs do not match")
		}
	}
	if err := agent.ValidateJSON(dispatch.Task.InputSchema, input); err != nil {
		return fmt.Errorf("multiagent: dispatch input: %w", err)
	}
	if handoff != nil && len(handoff.RequiredOutputSchema) > 0 {
		if len(dispatch.Task.OutputSchema) == 0 || !jsonEqual(handoff.RequiredOutputSchema, dispatch.Task.OutputSchema) {
			return fmt.Errorf("multiagent: handoff required output schema does not match task output schema")
		}
		if len(dispatch.OutputPolicy.Schema) > 0 && !jsonEqual(handoff.RequiredOutputSchema, dispatch.OutputPolicy.Schema) {
			return fmt.Errorf("multiagent: handoff required output schema does not match output policy")
		}
	}
	return nil
}

func jsonEqual(left, right json.RawMessage) bool {
	var leftCompact, rightCompact bytes.Buffer
	if json.Compact(&leftCompact, left) != nil || json.Compact(&rightCompact, right) != nil {
		return bytes.Equal(left, right)
	}
	return bytes.Equal(leftCompact.Bytes(), rightCompact.Bytes())
}
