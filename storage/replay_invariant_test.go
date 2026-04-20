package storage

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/internal/blackboard"
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

func TestReplayTeamParsesJSONEncodedLifecyclePayloads(t *testing.T) {
	t.Parallel()

	events := []Event{
		{
			RunID:  "team-json",
			TeamID: "team-json",
			Type:   EventTeamStarted,
			Payload: map[string]any{
				"pattern": "deepsearch",
				"phase":   string(team.PhaseResearch),
				"supervisor": map[string]any{
					"id":          "supervisor",
					"role":        string(team.RoleSupervisor),
					"profileName": "supervisor",
				},
				"workers": []any{
					map[string]any{
						"id":          "worker-1",
						"role":        string(team.RoleResearcher),
						"profileName": "researcher",
					},
				},
			},
		},
		{
			RunID:   "team-json",
			TeamID:  "team-json",
			TaskID:  "task-1",
			Type:    EventTaskScheduled,
			Payload: map[string]any{"title": "branch", "status": string(team.TaskStatusPending), "kind": string(team.TaskKindResearch)},
		},
		{
			RunID:   "team-json",
			TeamID:  "team-json",
			TaskID:  "task-1",
			Type:    EventTaskStarted,
			Payload: map[string]any{"attempts": 1},
		},
		{
			RunID:   "team-json",
			TeamID:  "team-json",
			TaskID:  "task-1",
			Type:    EventTaskCompleted,
			Payload: map[string]any{"status": string(team.TaskStatusCompleted), "summary": "done", "attempts": 1},
		},
		{
			RunID:  "team-json",
			TeamID: "team-json",
			TaskID: "task-1",
			Type:   EventTaskOutputsPublished,
			Payload: map[string]any{
				"sources": []any{
					map[string]any{"id": "source-1", "taskId": "task-1", "title": "branch"},
				},
				"artifacts": []any{
					map[string]any{"id": "artifact-1", "taskId": "task-1", "name": "branch", "content": "done"},
				},
				"evidence": []any{
					map[string]any{"id": "evidence-1", "taskId": "task-1", "sourceId": "source-1", "artifactId": "artifact-1", "summary": "done", "snippet": "done", "score": 0.85},
				},
				"claims": []any{
					map[string]any{"id": "claim-1", "taskId": "task-1", "summary": "done", "evidenceIds": []any{"evidence-1"}, "confidence": 0.85},
				},
				"findings": []any{
					map[string]any{"id": "finding-1", "taskId": "task-1", "summary": "done", "claimIds": []any{"claim-1"}, "evidenceIds": []any{"evidence-1"}, "confidence": 0.85},
				},
				"exchanges": []any{
					map[string]any{
						"id":         "exchange-1",
						"key":        "research.task-1",
						"namespace":  "task-1",
						"taskId":     "task-1",
						"version":    1,
						"etag":       "etag-1",
						"valueType":  string(blackboard.ExchangeValueTypeFindingRef),
						"text":       "done",
						"claimIds":   []any{"claim-1"},
						"findingIds": []any{"finding-1"},
						"metadata":   map[string]any{"kind": "research"},
					},
				},
			},
		},
		{
			RunID:   "team-json",
			TeamID:  "team-json",
			Type:    EventTeamCompleted,
			Payload: map[string]any{"summary": "done"},
		},
	}

	data, err := json.Marshal(events)
	if err != nil {
		t.Fatalf("marshal events: %v", err)
	}
	var decoded []Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal events: %v", err)
	}

	state := ReplayTeam(decoded)
	if state.Supervisor.ID != "supervisor" || state.Supervisor.ProfileName != "supervisor" {
		t.Fatalf("expected supervisor to survive JSON replay, got %#v", state.Supervisor)
	}
	if len(state.Workers) != 1 || state.Workers[0].ID != "worker-1" {
		t.Fatalf("expected workers to survive JSON replay, got %#v", state.Workers)
	}
	if state.Blackboard == nil {
		t.Fatalf("expected blackboard to survive JSON replay")
	}
	if got := len(state.Blackboard.Sources); got != 1 {
		t.Fatalf("expected replayed sources, got %#v", state.Blackboard)
	}
	if got := len(state.Blackboard.Claims); got != 1 {
		t.Fatalf("expected replayed claims, got %#v", state.Blackboard)
	}
	if got := len(state.Blackboard.Findings); got != 1 {
		t.Fatalf("expected replayed findings, got %#v", state.Blackboard)
	}
	if got := len(state.Blackboard.Exchanges); got != 1 || state.Blackboard.Exchanges[0].Namespace != "task-1" || state.Blackboard.Exchanges[0].ETag != "etag-1" {
		t.Fatalf("expected replayed exchange metadata, got %#v", state.Blackboard.Exchanges)
	}
}

func TestReplayInvariantRejectsCompletedToRunningTransition(t *testing.T) {
	t.Parallel()

	events := []Event{
		{RunID: "team-1", TeamID: "team-1", Type: EventTeamStarted, Payload: map[string]any{"pattern": "deepsearch"}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "task-1", Type: EventTaskScheduled, Payload: map[string]any{"status": string(team.TaskStatusPending), "taskVersion": 1}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "task-1", Type: EventTaskCompleted, Payload: map[string]any{"statusAfter": string(team.TaskStatusCompleted), "taskVersionAfter": 1}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "task-1", Type: EventTaskStarted, Payload: map[string]any{"statusBefore": string(team.TaskStatusCompleted), "statusAfter": string(team.TaskStatusRunning), "taskVersionBefore": 1, "taskVersionAfter": 1}},
	}

	result := ValidateReplay(events, ReplayTeam(events))
	if result.Valid {
		t.Fatalf("expected replay validation failure, got %#v", result)
	}
	if !containsMismatch(result.Mismatches, ReplayMismatchSemanticInconsistency, "completed -> running") {
		t.Fatalf("expected completed->running invariant failure, got %#v", result.Mismatches)
	}
}

func TestReplayInvariantRejectsTaskVersionRegressionAndDuplicateCompletion(t *testing.T) {
	t.Parallel()

	events := []Event{
		{RunID: "team-1", TeamID: "team-1", Type: EventTeamStarted, Payload: map[string]any{"pattern": "deepsearch"}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "task-1", Type: EventTaskScheduled, Payload: map[string]any{"status": string(team.TaskStatusPending), "taskVersion": 2}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "task-1", Type: EventTaskCompleted, Payload: map[string]any{"statusAfter": string(team.TaskStatusCompleted), "taskVersionAfter": 2}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "task-1", Type: EventTaskCompleted, Payload: map[string]any{"statusAfter": string(team.TaskStatusCompleted), "taskVersionAfter": 2}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "task-1", Type: EventTaskFailed, Payload: map[string]any{"statusAfter": string(team.TaskStatusFailed), "taskVersionAfter": 1}},
	}

	result := ValidateReplay(events, ReplayTeam(events))
	if result.Valid {
		t.Fatalf("expected replay validation failure, got %#v", result)
	}
	if !containsMismatch(result.Mismatches, ReplayMismatchSemanticInconsistency, "multiple authoritative completion") {
		t.Fatalf("expected duplicate completion invariant failure, got %#v", result.Mismatches)
	}
	if !containsMismatch(result.Mismatches, ReplayMismatchSemanticInconsistency, "monotonically increase") {
		t.Fatalf("expected version monotonicity invariant failure, got %#v", result.Mismatches)
	}
}

func TestReplayInvariantRejectsBlackboardExchangeFromIncompleteTask(t *testing.T) {
	t.Parallel()

	state := team.RunState{
		ID:      "team-1",
		Pattern: "deepsearch",
		Status:  team.StatusRunning,
		Phase:   team.PhaseResearch,
		Tasks: []team.Task{{
			ID:     "task-1",
			Status: team.TaskStatusPending,
		}},
		Blackboard: &blackboard.State{
			Exchanges: []blackboard.Exchange{{
				ID:     "exchange-1",
				TaskID: "task-1",
				Key:    "branch.result",
			}},
		},
	}

	result := ValidateReplay(nil, state)
	if result.Valid {
		t.Fatalf("expected blackboard provenance failure, got %#v", result)
	}
	if !containsMismatch(result.Mismatches, ReplayMismatchSemanticInconsistency, "trace back to a completed task") {
		t.Fatalf("expected blackboard provenance mismatch, got %#v", result.Mismatches)
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
