package storage

import (
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/team"
)

func TestReplayInvariantStateEquivalent(t *testing.T) {
	t.Parallel()

	events := replayInvariantFixtureEvents()
	authoritativeState := ReplayTeam(events)

	result := ValidateReplay(events, authoritativeState)
	if !result.Valid {
		t.Fatalf("expected replay validation to pass, got %#v", result)
	}
	if !result.ReplayConsistent {
		t.Fatalf("expected replay consistency to be true")
	}
}

func TestReplayRequiredEventSubset(t *testing.T) {
	t.Parallel()

	events := []Event{
		{RunID: "team-1", TeamID: "team-1", Type: EventTeamStarted, Payload: map[string]any{"pattern": "deepsearch"}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "task-1", Type: EventTaskScheduled, Payload: map[string]any{"title": "branch", "status": string(team.TaskStatusPending)}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "task-1", Type: EventTaskCompleted, Payload: map[string]any{"status": string(team.TaskStatusCompleted), "summary": "done"}},
		{RunID: "team-1", TeamID: "team-1", Type: EventTeamCompleted, Payload: map[string]any{"summary": "done"}},
	}
	authoritativeState := team.RunState{
		ID:      "team-1",
		Pattern: "deepsearch",
		Status:  team.StatusCompleted,
		Phase:   team.PhaseComplete,
		Tasks: []team.Task{{
			ID:     "task-1",
			Title:  "branch",
			Status: team.TaskStatusCompleted,
			Result: &team.Result{Summary: "done"},
		}},
		Result: &team.Result{Summary: "done"},
	}

	result := ValidateReplay(events, authoritativeState)
	if result.Valid {
		t.Fatalf("expected replay validation to fail for missing required events")
	}
	if !containsMismatch(result.Mismatches, ReplayMismatchMissingEvent, "TaskStarted") {
		t.Fatalf("expected missing TaskStarted mismatch, got %#v", result.Mismatches)
	}
	if !containsMismatch(result.Mismatches, ReplayMismatchMissingEvent, "TaskOutputsPublished") {
		t.Fatalf("expected missing TaskOutputsPublished mismatch, got %#v", result.Mismatches)
	}
}

func TestReplayMismatchReport(t *testing.T) {
	t.Parallel()

	events := []Event{
		{RunID: "team-1", TeamID: "team-1", Type: EventTeamStarted, Payload: map[string]any{"pattern": "deepsearch"}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "task-1", Type: EventTaskCompleted, Payload: map[string]any{"status": string(team.TaskStatusCompleted), "summary": "wrong-order"}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "task-1", Type: EventTaskStarted, Payload: map[string]any{"attempts": 1}},
		{RunID: "team-1", TeamID: "team-1", Type: EventTeamCompleted, Payload: map[string]any{"summary": "wrong-order"}},
	}
	authoritativeState := team.RunState{
		ID:      "team-1",
		Pattern: "deepsearch",
		Status:  team.StatusCompleted,
		Phase:   team.PhaseComplete,
		Tasks: []team.Task{{
			ID:     "task-1",
			Status: team.TaskStatusCompleted,
			Result: &team.Result{Summary: "expected-summary"},
		}},
		Result: &team.Result{Summary: "expected-summary"},
	}

	result := ValidateReplay(events, authoritativeState)
	if result.Valid {
		t.Fatalf("expected replay validation to fail")
	}
	if !containsMismatch(result.Mismatches, ReplayMismatchWrongOrder, "TaskStarted") {
		t.Fatalf("expected wrong-order mismatch, got %#v", result.Mismatches)
	}
	if !containsMismatch(result.Mismatches, ReplayMismatchStateMismatch, "authoritative state") {
		t.Fatalf("expected state mismatch message, got %#v", result.Mismatches)
	}
}

func replayInvariantFixtureEvents() []Event {
	return []Event{
		{RunID: "team-1", TeamID: "team-1", Type: EventTeamStarted, Payload: map[string]any{"pattern": "deepsearch", "phase": string(team.PhaseResearch)}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "task-1", Type: EventTaskScheduled, Payload: map[string]any{"title": "branch", "status": string(team.TaskStatusPending), "kind": string(team.TaskKindResearch)}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "task-1", Type: EventTaskStarted, Payload: map[string]any{"attempts": 1}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "task-1", Type: EventTaskCompleted, Payload: map[string]any{"status": string(team.TaskStatusCompleted), "summary": "done", "attempts": 1}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "task-1", Type: EventTaskOutputsPublished, Payload: map[string]any{}},
		{RunID: "team-1", TeamID: "team-1", Type: EventTeamCompleted, Payload: map[string]any{"summary": "done"}},
	}
}

func containsMismatch(mismatches []ReplayMismatch, mismatchType ReplayMismatchType, fragment string) bool {
	for _, mismatch := range mismatches {
		if mismatch.Type == mismatchType && strings.Contains(mismatch.Message, fragment) {
			return true
		}
	}
	return false
}
