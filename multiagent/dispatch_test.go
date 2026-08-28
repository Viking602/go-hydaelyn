package multiagent

import (
	"encoding/json"
	"testing"

	"github.com/Viking602/venat/api"
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

func TestValidateDispatchRequiresExecutableIdentity(t *testing.T) {
	tests := []struct {
		name    string
		current Dispatch
		wantErr bool
	}{
		{
			name:    "target required",
			current: Dispatch{Task: api.Task{ID: "run-1-worker", RunID: "run-1"}},
			wantErr: true,
		},
		{
			name: "retry task requires explicit class",
			current: Dispatch{
				To:   "agent-1",
				Task: api.Task{ID: "run-1-worker-attempt-2", RunID: "run-1"},
			},
			wantErr: true,
		},
		{
			name: "explicit retry class is unambiguous",
			current: Dispatch{
				To:        "agent-1",
				ClassName: "worker",
				Task:      api.Task{ID: "run-1-worker-attempt-2", RunID: "run-1"},
			},
		},
		{
			name:    "skip placeholder needs no target",
			current: Dispatch{Skip: true},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateDispatch(test.current)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateDispatch() error = %v, wantErr %t", err, test.wantErr)
			}
		})
	}
}
