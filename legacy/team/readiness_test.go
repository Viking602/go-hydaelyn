package team

import (
	"testing"

	"github.com/Viking602/go-hydaelyn/internal/blackboard"
)

func TestRunnableCandidates_DependencyPendingIsBlocked(t *testing.T) {
	state := RunState{
		Tasks: []Task{
			{ID: "task-1", Status: TaskStatusRunning},
			{ID: "task-2", Status: TaskStatusPending, DependsOn: []string{"task-1"}},
		},
	}
	state.Normalize()

	candidates := state.RunnableCandidates()
	if len(candidates) != 1 {
		t.Fatalf("expected one pending candidate, got %d", len(candidates))
	}
	got := candidates[0]
	if got.Task.ID != "task-2" || got.Ready {
		t.Fatalf("expected task-2 blocked, got %#v", got)
	}
	if len(got.Blockers) != 1 || got.Blockers[0].Kind != BlockerDependencyPending || got.Blockers[0].Target != "task-1" {
		t.Fatalf("expected dependency_pending blocker on task-1, got %#v", got.Blockers)
	}
}

func TestRunnableCandidates_DependencyCompletedIsReady(t *testing.T) {
	state := RunState{
		Tasks: []Task{
			{ID: "task-1", Status: TaskStatusCompleted},
			{ID: "task-2", Status: TaskStatusPending, DependsOn: []string{"task-1"}},
		},
	}
	state.Normalize()

	candidates := state.RunnableCandidates()
	if len(candidates) != 1 || candidates[0].Task.ID != "task-2" {
		t.Fatalf("expected one pending candidate for task-2, got %#v", candidates)
	}
	if !candidates[0].Ready || len(candidates[0].Blockers) != 0 {
		t.Fatalf("expected task-2 ready after dependency completes, got %#v", candidates[0])
	}
}

func TestRunnableCandidates_DependencyFailureSurfaced(t *testing.T) {
	state := RunState{
		Tasks: []Task{
			{ID: "task-1", Status: TaskStatusFailed},
			{ID: "task-2", Status: TaskStatusPending, DependsOn: []string{"task-1"}},
		},
	}
	state.Normalize()

	candidates := state.RunnableCandidates()
	if len(candidates) != 1 {
		t.Fatalf("expected one pending candidate, got %d", len(candidates))
	}
	if candidates[0].Ready {
		t.Fatalf("expected task-2 to be blocked by failed dependency, got ready=true")
	}
	if candidates[0].Blockers[0].Kind != BlockerDependencyFailed {
		t.Fatalf("expected dependency_failed blocker, got %#v", candidates[0].Blockers)
	}
}

func TestRunnableCandidates_DependencyMissingSurfaced(t *testing.T) {
	state := RunState{
		Tasks: []Task{
			{ID: "task-2", Status: TaskStatusPending, DependsOn: []string{"ghost"}},
		},
	}
	state.Normalize()

	candidates := state.RunnableCandidates()
	if len(candidates) != 1 {
		t.Fatalf("expected one pending candidate, got %d", len(candidates))
	}
	blockers := candidates[0].Blockers
	if len(blockers) != 1 || blockers[0].Kind != BlockerDependencyMissing || blockers[0].Target != "ghost" {
		t.Fatalf("expected dependency_missing for ghost, got %#v", blockers)
	}
}

func TestRunnableCandidates_ReadMissingBlocksWhenBlackboardEmpty(t *testing.T) {
	state := RunState{
		Blackboard: &blackboard.State{},
		Tasks: []Task{
			{ID: "task-1", Status: TaskStatusPending, Reads: []string{"research.notes"}},
		},
	}
	state.Normalize()

	candidates := state.RunnableCandidates()
	if len(candidates) != 1 || candidates[0].Ready {
		t.Fatalf("expected task-1 blocked on missing read, got %#v", candidates)
	}
	blockers := candidates[0].Blockers
	if len(blockers) != 1 || blockers[0].Kind != BlockerReadMissing || blockers[0].Target != "research.notes" {
		t.Fatalf("expected read_missing blocker on research.notes, got %#v", blockers)
	}
}

func TestRunnableCandidates_ReadSatisfiedUnblocksTask(t *testing.T) {
	board := &blackboard.State{}
	if _, err := board.UpsertExchangeCAS(blackboard.Exchange{
		Key:       "research.notes",
		Namespace: "research.notes",
		TaskID:    "task-source",
		Version:   1,
		ValueType: blackboard.ExchangeValueTypeText,
		Text:      "some notes",
	}); err != nil {
		t.Fatalf("UpsertExchangeCAS() error = %v", err)
	}
	state := RunState{
		Blackboard: board,
		Tasks: []Task{
			{ID: "task-1", Status: TaskStatusPending, Reads: []string{"research.notes"}},
		},
	}
	state.Normalize()

	candidates := state.RunnableCandidates()
	if len(candidates) != 1 {
		t.Fatalf("expected one pending candidate, got %d", len(candidates))
	}
	if !candidates[0].Ready {
		t.Fatalf("expected task-1 ready once read is published, got %#v", candidates[0])
	}
}

func TestRunnableCandidates_NoBlackboardBlocksDeclaredReads(t *testing.T) {
	// Declared Reads without a blackboard cannot possibly be satisfied — the
	// task would execute blind. The resolver must flag it rather than tacitly
	// pretending the reads are optional.
	state := RunState{
		Tasks: []Task{
			{ID: "task-1", Status: TaskStatusPending, Reads: []string{"research.notes"}},
		},
	}
	state.Normalize()

	candidates := state.RunnableCandidates()
	if len(candidates) != 1 || candidates[0].Ready {
		t.Fatalf("expected task-1 blocked without blackboard, got %#v", candidates)
	}
	if candidates[0].Blockers[0].Kind != BlockerReadMissing {
		t.Fatalf("expected read_missing blocker, got %#v", candidates[0].Blockers)
	}
}

func TestRunnableCandidates_RequiredSelectorBlocksWhenNotSatisfied(t *testing.T) {
	state := RunState{
		Blackboard: &blackboard.State{},
		Tasks: []Task{
			{
				ID:     "task-1",
				Status: TaskStatusPending,
				ReadSelectors: []blackboard.ExchangeSelector{
					{Keys: []string{"research.notes"}, RequireVerified: true, Required: true, Label: "research.notes"},
				},
			},
		},
	}
	state.Normalize()

	candidates := state.RunnableCandidates()
	if len(candidates) != 1 || candidates[0].Ready {
		t.Fatalf("expected required selector to block task-1, got %#v", candidates)
	}
	blockers := candidates[0].Blockers
	if len(blockers) != 1 || blockers[0].Kind != BlockerReadMissing || blockers[0].Target != "research.notes" {
		t.Fatalf("expected read_missing for selector label research.notes, got %#v", blockers)
	}
}

func TestRunnableCandidates_OptionalSelectorDoesNotBlock(t *testing.T) {
	state := RunState{
		Blackboard: &blackboard.State{},
		Tasks: []Task{
			{
				ID:     "task-1",
				Status: TaskStatusPending,
				ReadSelectors: []blackboard.ExchangeSelector{
					{Keys: []string{"research.notes"}, Label: "research.notes"},
				},
			},
		},
	}
	state.Normalize()

	candidates := state.RunnableCandidates()
	if len(candidates) != 1 || !candidates[0].Ready {
		t.Fatalf("expected optional selector to leave task ready, got %#v", candidates)
	}
}

func TestRunnableCandidates_NonPendingTasksIgnored(t *testing.T) {
	state := RunState{
		Tasks: []Task{
			{ID: "task-1", Status: TaskStatusCompleted},
			{ID: "task-2", Status: TaskStatusRunning},
			{ID: "task-3", Status: TaskStatusFailed},
		},
	}
	state.Normalize()

	if got := state.RunnableCandidates(); len(got) != 0 {
		t.Fatalf("expected resolver to skip non-pending tasks, got %#v", got)
	}
}
