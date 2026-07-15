package multiagent

import (
	"encoding/json"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
)

func TestValidateDispatchTypedHandoff(t *testing.T) {
	dispatch := Dispatch{
		To:    "agent-2",
		Task:  api.Task{RunID: "run-1", InputSchema: json.RawMessage(`{"type":"object","required":["value"],"properties":{"value":{"type":"string"}}}`)},
		Input: json.RawMessage(`{"value":"ok"}`),
		Handoff: &Handoff{
			RunID:   "run-1",
			From:    "agent-1",
			To:      "agent-2",
			Payload: json.RawMessage(`{"value":"ok"}`),
		},
	}
	if err := ValidateDispatch(dispatch); err != nil {
		t.Fatalf("ValidateDispatch() error = %v", err)
	}
	dispatch.Handoff.Payload = json.RawMessage(`{"value":1}`)
	if err := ValidateDispatch(dispatch); err == nil {
		t.Fatal("ValidateDispatch() accepted a mismatched handoff payload")
	}
	dispatch.Handoff = nil
	dispatch.Task.Input = json.RawMessage(`{"value":1}`)
	if err := ValidateDispatch(dispatch); err == nil {
		t.Fatal("ValidateDispatch() accepted a non-handoff input that violates the task schema")
	}
}
