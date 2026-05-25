package multiagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/api"
)

func TestComputeInstanceIDIsDeterministicAndNormalizesInputs(t *testing.T) {
	first := ComputeInstanceID(" researcher ", " run-1 ", " task-1 ", " shard-a ")
	second := ComputeInstanceID("researcher", "run-1", "task-1", "shard-a")
	other := ComputeInstanceID("researcher", "run-1", "task-1", "shard-b")

	if first != second {
		t.Fatalf("ComputeInstanceID should trim and remain deterministic: %q != %q", first, second)
	}
	if first == other {
		t.Fatalf("ComputeInstanceID should include suffix in the stable identity: %q", first)
	}
	if !strings.HasPrefix(first, "ai-") || len(first) != len("ai-")+16 {
		t.Fatalf("ComputeInstanceID format = %q, want ai- plus 16 hex chars", first)
	}
}

func TestTypedHandoffJSONContractPreservesSchemasAndEvidence(t *testing.T) {
	createdAt := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	handoff := Handoff{
		ID:                   "handoff-1",
		RunID:                "run-1",
		From:                 "agent-a",
		To:                   "agent-b",
		Reason:               "needs specialist",
		Payload:              json.RawMessage(`{"claim":"needs-db"}`),
		EvidenceIDs:          []string{"evidence-1", "evidence-2"},
		RequiredOutputSchema: json.RawMessage(`{"type":"object"}`),
		CreatedAt:            createdAt,
	}

	payload, err := json.Marshal(handoff)
	if err != nil {
		t.Fatalf("Marshal(Handoff) error = %v", err)
	}
	var decoded Handoff
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("Unmarshal(Handoff) error = %v", err)
	}

	if decoded.ID != handoff.ID || decoded.RunID != handoff.RunID || decoded.From != handoff.From || decoded.To != handoff.To {
		t.Fatalf("decoded identity = %#v", decoded)
	}
	if string(decoded.Payload) != string(handoff.Payload) || string(decoded.RequiredOutputSchema) != string(handoff.RequiredOutputSchema) {
		t.Fatalf("decoded payload/schema = %#v", decoded)
	}
	if len(decoded.EvidenceIDs) != 2 || decoded.EvidenceIDs[1] != "evidence-2" || !decoded.CreatedAt.Equal(createdAt) {
		t.Fatalf("decoded evidence/time = %#v", decoded)
	}
}

func TestSchedulerContractAcceptsTeamStateSnapshot(t *testing.T) {
	scheduler := SchedulerFunc(func(_ context.Context, state TeamState) ([]Dispatch, error) {
		return []Dispatch{{To: state.Instances[0].ID, Task: state.Tasks[0]}}, nil
	})
	state := TeamState{
		RunID:     "run-1",
		Tasks:     []api.Task{{ID: "task-1", RunID: "run-1"}},
		Instances: []AgentInstance{{ID: "agent-1", ClassName: "researcher", RunID: "run-1"}},
	}

	dispatches, err := scheduler.Next(context.Background(), state)
	if err != nil {
		t.Fatalf("Scheduler.Next() error = %v", err)
	}
	if len(dispatches) != 1 || dispatches[0].To != "agent-1" || dispatches[0].Task.ID != "task-1" {
		t.Fatalf("dispatches = %#v", dispatches)
	}
}

type SchedulerFunc func(context.Context, TeamState) ([]Dispatch, error)

func (fn SchedulerFunc) Next(ctx context.Context, state TeamState) ([]Dispatch, error) {
	return fn(ctx, state)
}
