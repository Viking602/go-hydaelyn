package storage

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/internal/blackboard"
	"github.com/Viking602/go-hydaelyn/team"
)

func TestMemoryEventStoreAppendAndList(t *testing.T) {
	driver := NewMemoryDriver()
	store := driver.Events()
	events := []Event{
		{RunID: "team-1", Sequence: 1, Type: EventTeamStarted, TeamID: "team-1"},
		{RunID: "team-1", Sequence: 2, Type: EventTaskCompleted, TeamID: "team-1", TaskID: "task-1"},
	}
	for _, event := range events {
		if err := store.Append(context.Background(), event); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}
	got, err := store.List(context.Background(), "team-1")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 2 || got[1].Type != EventTaskCompleted {
		t.Fatalf("unexpected events %#v", got)
	}
}

func TestReplayRebuildsCompletedState(t *testing.T) {
	events := []Event{
		{RunID: "team-1", Sequence: 1, Type: EventTeamStarted, TeamID: "team-1", Payload: map[string]any{"pattern": "deepsearch"}},
		{RunID: "team-1", Sequence: 2, Type: EventTaskScheduled, TeamID: "team-1", TaskID: "task-1", Payload: map[string]any{"title": "branch", "input": "branch", "status": string(team.TaskStatusPending)}},
		{RunID: "team-1", Sequence: 3, Type: EventTaskStarted, TeamID: "team-1", TaskID: "task-1"},
		{RunID: "team-1", Sequence: 4, Type: EventTaskCompleted, TeamID: "team-1", TaskID: "task-1", Payload: map[string]any{"summary": "branch", "status": string(team.TaskStatusCompleted)}},
		{RunID: "team-1", Sequence: 5, Type: EventTeamCompleted, TeamID: "team-1", Payload: map[string]any{"summary": "done"}},
	}
	state := ReplayTeam(events)
	if state.ID != "team-1" || state.Status != team.StatusCompleted {
		t.Fatalf("unexpected replayed state %#v", state)
	}
	if len(state.Tasks) != 1 || state.Tasks[0].Status != team.TaskStatusCompleted {
		t.Fatalf("unexpected replayed tasks %#v", state.Tasks)
	}
	if state.Result == nil || state.Result.Summary != "done" {
		t.Fatalf("expected replayed result, got %#v", state.Result)
	}
}

func TestReplayRebuildsPanelTaskBoard(t *testing.T) {
	events := []Event{
		{RunID: "team-1", Sequence: 1, Type: EventTeamStarted, TeamID: "team-1", Payload: map[string]any{"pattern": "panel", "interactionMode": string(team.InteractionModePanel)}},
		{RunID: "team-1", Sequence: 2, Type: EventTodoPlanned, TeamID: "team-1", Payload: map[string]any{"planId": "team-1-todo-plan", "goal": "launch auth", "mode": string(team.InteractionModePanel)}},
		{RunID: "team-1", Sequence: 3, Type: EventTodoClaimed, TeamID: "team-1", TaskID: "security-review", Payload: map[string]any{
			"todoId":               "security-review",
			"title":                "review auth threat model",
			"domain":               "security",
			"priority":             string(team.TodoPriorityHigh),
			"agentId":              "worker-1",
			"requiredCapabilities": []any{"threat_model"},
			"expectedReportKind":   string(team.ReportKindResearch),
			"verificationPolicy": map[string]any{
				"required":      true,
				"mode":          "cross_review",
				"minConfidence": 0.8,
				"reviewers":     float64(2),
			},
			"status": string(team.TodoStatusClaimed),
		}},
		{RunID: "team-1", Sequence: 4, Type: EventTaskScheduled, TeamID: "team-1", TaskID: "security-review", Payload: map[string]any{
			"title":              "review auth threat model",
			"status":             string(team.TaskStatusPending),
			"kind":               string(team.TaskKindResearch),
			"stage":              string(team.TaskStageImplement),
			"todoId":             "security-review",
			"assigneeAgent":      "worker-1",
			"expectedReportKind": string(team.ReportKindResearch),
		}},
		{RunID: "team-1", Sequence: 5, Type: EventTaskStarted, TeamID: "team-1", TaskID: "security-review", Payload: map[string]any{"statusAfter": string(team.TaskStatusRunning)}},
		{RunID: "team-1", Sequence: 6, Type: EventTaskCompleted, TeamID: "team-1", TaskID: "security-review", Payload: map[string]any{"statusAfter": string(team.TaskStatusCompleted), "summary": "research done"}},
		{RunID: "team-1", Sequence: 7, Type: EventTaskScheduled, TeamID: "team-1", TaskID: "security-review-review", Payload: map[string]any{
			"title":         "review auth threat model",
			"status":        string(team.TaskStatusPending),
			"kind":          string(team.TaskKindVerify),
			"stage":         string(team.TaskStageReview),
			"todoId":        "security-review",
			"assigneeAgent": "worker-2",
			"readSelectors": []any{map[string]any{
				"taskIds":           []any{"security-review"},
				"valueTypes":        []any{string(blackboard.ExchangeValueTypeFindingRef)},
				"includeText":       true,
				"includeStructured": true,
				"includeArtifacts":  true,
				"required":          true,
				"label":             "panel research output",
			}},
		}},
		{RunID: "team-1", Sequence: 8, Type: EventTaskCompleted, TeamID: "team-1", TaskID: "security-review-review", Payload: map[string]any{"statusAfter": string(team.TaskStatusCompleted), "summary": "supported"}},
	}

	state := ReplayTeam(events)
	if state.InteractionMode != team.InteractionModePanel {
		t.Fatalf("expected panel interaction mode, got %#v", state.InteractionMode)
	}
	if state.TaskBoard == nil || len(state.TaskBoard.Plan.Items) != 1 {
		t.Fatalf("expected replayed task board, got %#v", state.TaskBoard)
	}
	todo := state.TaskBoard.Plan.Items[0]
	if todo.ID != "security-review" || todo.PrimaryAgentID != "worker-1" || todo.Status != team.TodoStatusVerified {
		t.Fatalf("expected replayed claimed and verified todo, got %#v", todo)
	}
	if todo.TaskID != "security-review" || len(todo.ReviewTaskIDs) != 1 || todo.ReviewTaskIDs[0] != "security-review-review" {
		t.Fatalf("expected replayed todo task links, got %#v", todo)
	}
	if todo.VerificationPolicy.Reviewers != 2 || !todo.VerificationPolicy.Required || todo.VerificationPolicy.MinConfidence != 0.8 {
		t.Fatalf("expected replayed todo verification policy, got %#v", todo.VerificationPolicy)
	}
	if len(state.Tasks) != 2 || state.Tasks[0].TodoID != "security-review" || state.Tasks[0].ExpectedReportKind != team.ReportKindResearch {
		t.Fatalf("expected replayed panel task fields, got %#v", state.Tasks)
	}
	if len(state.Tasks[1].ReadSelectors) != 1 || state.Tasks[1].ReadSelectors[0].TaskIDs[0] != "security-review" || !state.Tasks[1].ReadSelectors[0].Required {
		t.Fatalf("expected replayed review read selector, got %#v", state.Tasks[1].ReadSelectors)
	}
}
