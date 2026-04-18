package planner

import (
	"encoding/json"
	"testing"

	"github.com/Viking602/go-hydaelyn/team"
)

func TestPlanTasksCarryCollaborationMetadata(t *testing.T) {
	plan := Plan{
		Goal: "ship collaboration contract",
		Tasks: []TaskSpec{
			{
				ID:               "implement-1",
				Kind:             string(team.TaskKindResearch),
				Stage:            team.TaskStageImplement,
				Namespace:        "impl.implement-1",
				VerifierRequired: true,
			},
			{
				ID:   "legacy-1",
				Kind: string(team.TaskKindResearch),
			},
		},
	}

	payload, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		t.Fatalf("json.Unmarshal(raw) error = %v", err)
	}
	tasks, ok := raw["tasks"].([]any)
	if !ok || len(tasks) != 2 {
		t.Fatalf("expected two serialized tasks, got %#v", raw["tasks"])
	}
	first, ok := tasks[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first task object, got %#v", tasks[0])
	}
	if got := first["stage"]; got != string(team.TaskStageImplement) {
		t.Fatalf("expected stage %q, got %#v", team.TaskStageImplement, got)
	}
	if got := first["namespace"]; got != "impl.implement-1" {
		t.Fatalf("expected namespace to round-trip, got %#v", got)
	}
	if got := first["verifierRequired"]; got != true {
		t.Fatalf("expected verifierRequired=true, got %#v", got)
	}
	second, ok := tasks[1].(map[string]any)
	if !ok {
		t.Fatalf("expected second task object, got %#v", tasks[1])
	}
	if _, exists := second["stage"]; exists {
		t.Fatalf("expected stage to be omitted for legacy task, got %#v", second)
	}
	if _, exists := second["namespace"]; exists {
		t.Fatalf("expected namespace to be omitted for legacy task, got %#v", second)
	}
	if _, exists := second["verifierRequired"]; exists {
		t.Fatalf("expected verifierRequired to be omitted for legacy task, got %#v", second)
	}

	var decoded Plan
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(plan) error = %v", err)
	}
	if len(decoded.Tasks) != 2 {
		t.Fatalf("expected two decoded tasks, got %#v", decoded.Tasks)
	}
	if decoded.Tasks[0].Stage != team.TaskStageImplement {
		t.Fatalf("expected decoded stage %q, got %q", team.TaskStageImplement, decoded.Tasks[0].Stage)
	}
	if decoded.Tasks[0].Namespace != "impl.implement-1" {
		t.Fatalf("expected decoded namespace, got %#v", decoded.Tasks[0].Namespace)
	}
	if !decoded.Tasks[0].VerifierRequired {
		t.Fatalf("expected decoded verifierRequired=true, got %#v", decoded.Tasks[0])
	}
}
