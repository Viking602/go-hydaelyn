package host

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/go-hydaelyn/blackboard"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

func TestQueuedStatePersistRejectsStaleVersion(t *testing.T) {
	driver := storage.NewMemoryDriver()
	runtime := New(Config{Storage: driver})
	runtime.RegisterPattern(linearPattern{})

	base := team.RunState{ID: "team-1", Pattern: "linear", Status: team.StatusCompleted, Phase: team.PhaseComplete}
	base.Normalize()
	if err := driver.Teams().Save(context.Background(), base); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	stale, err := driver.Teams().Load(context.Background(), base.ID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	newer := stale
	newer.Metadata = map[string]string{"winner": "newer"}
	if _, err := driver.Teams().SaveCAS(context.Background(), newer, stale.Version); err != nil {
		t.Fatalf("SaveCAS() error = %v", err)
	}
	err = runtime.persistQueuedState(context.Background(), stale, "")
	if !errors.Is(err, storage.ErrStaleState) {
		t.Fatalf("expected ErrStaleState, got %v", err)
	}
	current, err := driver.Teams().Load(context.Background(), base.ID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if current.Version != 2 {
		t.Fatalf("expected version 2 to remain authoritative, got %d", current.Version)
	}
	if got := current.Metadata["winner"]; got != "newer" {
		t.Fatalf("expected newer state to remain authoritative, got %q", got)
	}
}

func TestMultiAgentCollaboration_QueuedRetryIsIdempotent(t *testing.T) {
	runtime := New(Config{WorkerID: "worker-a"})
	state := team.RunState{
		ID:      "team-queued-idempotent",
		Pattern: "linear",
		Status:  team.StatusRunning,
		Phase:   team.PhaseResearch,
		Tasks: []team.Task{{
			ID:        "task-1",
			Kind:      team.TaskKindResearch,
			Namespace: "impl.task-1",
			Writes:    []string{"result"},
			Publish:   []team.OutputVisibility{team.OutputVisibilityBlackboard},
			Status:    team.TaskStatusPending,
		}},
	}
	state.Normalize()
	first := state.Tasks[0]
	first.Status = team.TaskStatusCompleted
	first.Result = &team.Result{Summary: "authoritative result"}
	state = runtime.applyQueuedTaskResult(state, 0, first)
	completedAt := state.Tasks[0].CompletedAt
	completedBy := state.Tasks[0].CompletedBy
	if completedAt.IsZero() || completedBy != "worker-a" {
		t.Fatalf("expected authoritative completion metadata, got %#v", state.Tasks[0])
	}
	duplicate := state.Tasks[0]
	duplicate.Result = &team.Result{Summary: "duplicate result must be ignored"}
	state = runtime.applyQueuedTaskResult(state, 0, duplicate)
	if state.Tasks[0].CompletedAt != completedAt || state.Tasks[0].CompletedBy != completedBy {
		t.Fatalf("expected retry to preserve original completion metadata, got %#v", state.Tasks[0])
	}
	if state.Tasks[0].Result == nil || state.Tasks[0].Result.Summary != "authoritative result" {
		t.Fatalf("expected retry to preserve authoritative result, got %#v", state.Tasks[0].Result)
	}
	if state.Blackboard == nil {
		t.Fatalf("expected blackboard output to exist")
	}
	assertSingleCommittedOutput(t, *state.Blackboard, "task-1", "result", "authoritative result")
}

func assertSingleCommittedOutput(t *testing.T, board blackboard.State, taskID, key, text string) {
	t.Helper()
	if got := len(board.ClaimsForTask(taskID)); got != 1 {
		t.Fatalf("expected 1 claim for %s, got %d", taskID, got)
	}
	if got := len(board.ExchangesForTask(taskID)); got != 1 {
		t.Fatalf("expected 1 exchange for %s, got %d", taskID, got)
	}
	exchanges := board.ExchangesForTask(taskID)
	if exchanges[0].Key != key || exchanges[0].Text != text {
		t.Fatalf("expected authoritative exchange %q=%q, got %#v", key, text, exchanges[0])
	}
}
