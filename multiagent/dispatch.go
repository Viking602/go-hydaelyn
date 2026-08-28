package multiagent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/api"
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
	if dispatch.Skip {
		return nil
	}
	if err := validateDispatchIdentity(dispatch); err != nil {
		return err
	}
	if err := validateDispatchHandoff(dispatch); err != nil {
		return err
	}
	input, err := canonicalDispatchInput(dispatch)
	if err != nil {
		return err
	}
	if err := agent.ValidateJSON(dispatch.Task.InputSchema, input); err != nil {
		return fmt.Errorf("multiagent: dispatch input: %w", err)
	}
	return validateDispatchOutput(dispatch)
}

func validateDispatchIdentity(dispatch Dispatch) error {
	if dispatch.To == "" {
		return fmt.Errorf("multiagent: dispatch target is required")
	}
	if dispatch.ClassName == "" && hasNumericAttemptSuffix(dispatch.Task.ID) {
		return fmt.Errorf("multiagent: dispatch class name is required for retry task %q", dispatch.Task.ID)
	}
	return nil
}

func validateDispatchHandoff(dispatch Dispatch) error {
	handoff := dispatch.Handoff
	if handoff == nil {
		return nil
	}
	if handoff.RunID != "" && handoff.RunID != dispatch.Task.RunID {
		return fmt.Errorf("multiagent: handoff run %q does not match task run %q", handoff.RunID, dispatch.Task.RunID)
	}
	if handoff.To != "" && handoff.To != dispatch.To {
		return fmt.Errorf("multiagent: handoff target %q does not match dispatch target %q", handoff.To, dispatch.To)
	}
	return nil
}

func canonicalDispatchInput(dispatch Dispatch) (json.RawMessage, error) {
	inputs := []json.RawMessage{dispatch.Task.Input, dispatch.Input}
	if dispatch.Handoff != nil {
		inputs = append(inputs, dispatch.Handoff.Payload)
	}
	var input json.RawMessage
	for _, candidate := range inputs {
		if len(candidate) == 0 {
			continue
		}
		if !json.Valid(candidate) {
			return nil, fmt.Errorf("multiagent: dispatch input is not valid JSON")
		}
		if len(input) == 0 {
			input = candidate
			continue
		}
		if !jsonEqual(input, candidate) {
			return nil, fmt.Errorf("multiagent: task, dispatch, and handoff inputs do not match")
		}
	}
	return input, nil
}

func validateDispatchOutput(dispatch Dispatch) error {
	handoff := dispatch.Handoff
	if handoff == nil || len(handoff.RequiredOutputSchema) == 0 {
		return nil
	}
	if len(dispatch.Task.OutputSchema) == 0 || !jsonEqual(handoff.RequiredOutputSchema, dispatch.Task.OutputSchema) {
		return fmt.Errorf("multiagent: handoff required output schema does not match task output schema")
	}
	if len(dispatch.OutputPolicy.Schema) > 0 && !jsonEqual(handoff.RequiredOutputSchema, dispatch.OutputPolicy.Schema) {
		return fmt.Errorf("multiagent: handoff required output schema does not match output policy")
	}
	return nil
}

func hasNumericAttemptSuffix(taskID string) bool {
	marker := strings.LastIndex(taskID, "-attempt-")
	if marker < 0 {
		return false
	}
	_, err := strconv.Atoi(taskID[marker+len("-attempt-"):])
	return err == nil
}

func jsonEqual(left, right json.RawMessage) bool {
	var leftCompact, rightCompact bytes.Buffer
	if json.Compact(&leftCompact, left) != nil || json.Compact(&rightCompact, right) != nil {
		return bytes.Equal(left, right)
	}
	return bytes.Equal(leftCompact.Bytes(), rightCompact.Bytes())
}
