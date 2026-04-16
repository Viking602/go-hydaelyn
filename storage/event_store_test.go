package storage

import (
	"context"
	"testing"

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
