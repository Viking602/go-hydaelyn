package state_test

import (
	"errors"
	"testing"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/state"
)

// ── IsTerminalRun ────────────────────────────────────────────────────────────

func TestIsTerminalRun(t *testing.T) {
	terminal := []model.RunStatus{
		model.RunStatusCompleted,
		model.RunStatusFailed,
		model.RunStatusCancelled,
	}
	for _, s := range terminal {
		if !state.IsTerminalRun(s) {
			t.Errorf("expected %q to be terminal", s)
		}
	}
	nonTerminal := []model.RunStatus{
		model.RunStatusCreated,
		model.RunStatusRunning,
		model.RunStatusPlanning,
		model.RunStatusBlocked,
		model.RunStatusWaitingApproval,
	}
	for _, s := range nonTerminal {
		if state.IsTerminalRun(s) {
			t.Errorf("expected %q to be non-terminal", s)
		}
	}
}

// ── IsTerminalTask ───────────────────────────────────────────────────────────

func TestIsTerminalTask(t *testing.T) {
	terminal := []model.TaskStatus{
		model.TaskStatusCompleted,
		model.TaskStatusFailed,
		model.TaskStatusCancelled,
	}
	for _, s := range terminal {
		if !state.IsTerminalTask(s) {
			t.Errorf("expected %q to be terminal", s)
		}
	}
	nonTerminal := []model.TaskStatus{
		model.TaskStatusCreated,
		model.TaskStatusRunning,
		model.TaskStatusDispatched,
		model.TaskStatusPaused,
		model.TaskStatusBlocked,
	}
	for _, s := range nonTerminal {
		if state.IsTerminalTask(s) {
			t.Errorf("expected %q to be non-terminal", s)
		}
	}
}

// ── TaskCanBecomeReady ───────────────────────────────────────────────────────

func TestTaskCanBecomeReady(t *testing.T) {
	ready := []model.TaskStatus{
		model.TaskStatusCreated,
		model.TaskStatusPlanned,
		model.TaskStatusValidated,
		model.TaskStatusRouted,
		model.TaskStatusWaitingDependency,
	}
	for _, s := range ready {
		if !state.TaskCanBecomeReady(s) {
			t.Errorf("expected %q to be able to become ready", s)
		}
	}
	notReady := []model.TaskStatus{
		model.TaskStatusDispatched,
		model.TaskStatusRunning,
		model.TaskStatusCompleted,
		model.TaskStatusFailed,
		model.TaskStatusCancelled,
	}
	for _, s := range notReady {
		if state.TaskCanBecomeReady(s) {
			t.Errorf("expected %q to NOT be able to become ready", s)
		}
	}
}

// ── TransitionRun ────────────────────────────────────────────────────────────

func TestTransitionRun_ValidTransition(t *testing.T) {
	run := model.Run{ID: "r1", Status: model.RunStatusCreated}
	next, err := state.TransitionRun(run, model.RunStatusPlanning)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next.Status != model.RunStatusPlanning {
		t.Errorf("want Planning, got %q", next.Status)
	}
}

func TestTransitionRun_SameStatus(t *testing.T) {
	run := model.Run{ID: "r1", Status: model.RunStatusRunning}
	next, err := state.TransitionRun(run, model.RunStatusRunning)
	if err != nil {
		t.Fatalf("same-status transition should not error: %v", err)
	}
	if next.Status != model.RunStatusRunning {
		t.Errorf("status should remain Running")
	}
}

func TestTransitionRun_InvalidTransition(t *testing.T) {
	run := model.Run{ID: "r1", Status: model.RunStatusCompleted}
	_, err := state.TransitionRun(run, model.RunStatusRunning)
	if !errors.Is(err, model.ErrTerminalState) {
		t.Fatalf("want ErrTerminalState, got %v", err)
	}
}

func TestTransitionRun_ForbiddenTransition(t *testing.T) {
	run := model.Run{ID: "r1", Status: model.RunStatusCreated}
	_, err := state.TransitionRun(run, model.RunStatusCompleted)
	if !errors.Is(err, model.ErrInvalidTransition) {
		t.Fatalf("want ErrInvalidTransition, got %v", err)
	}
}

// ── TransitionTask ───────────────────────────────────────────────────────────

func TestTransitionTask_ValidTransition(t *testing.T) {
	task := model.Task{ID: "t1", Status: model.TaskStatusDispatched, Version: 1}
	next, err := state.TransitionTask(task, model.TaskStatusRunning, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next.Status != model.TaskStatusRunning {
		t.Errorf("want Running, got %q", next.Status)
	}
	if next.Version != 2 {
		t.Errorf("version should be bumped to 2, got %d", next.Version)
	}
}

func TestTransitionTask_NoBumpVersion(t *testing.T) {
	task := model.Task{ID: "t1", Status: model.TaskStatusDispatched, Version: 5}
	next, err := state.TransitionTask(task, model.TaskStatusRunning, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next.Version != 5 {
		t.Errorf("version should remain 5, got %d", next.Version)
	}
}

func TestTransitionTask_TerminalBlocked(t *testing.T) {
	task := model.Task{ID: "t1", Status: model.TaskStatusCompleted}
	_, err := state.TransitionTask(task, model.TaskStatusRunning, false)
	if !errors.Is(err, model.ErrTerminalState) {
		t.Fatalf("want ErrTerminalState, got %v", err)
	}
}

func TestTransitionTask_ForbiddenTransition(t *testing.T) {
	task := model.Task{ID: "t1", Status: model.TaskStatusCreated}
	_, err := state.TransitionTask(task, model.TaskStatusCompleted, false)
	if !errors.Is(err, model.ErrInvalidTransition) {
		t.Fatalf("want ErrInvalidTransition, got %v", err)
	}
}

// ── DependencyGate ───────────────────────────────────────────────────────────

func TestDependencyGate_NoDeps(t *testing.T) {
	task := model.Task{ID: "t1"}
	ready, fatal := state.DependencyGate(task, nil)
	if !ready || fatal {
		t.Error("task with no deps should be ready and non-fatal")
	}
}

func TestDependencyGate_AllCompleted(t *testing.T) {
	dep := model.Task{ID: "dep1", Status: model.TaskStatusCompleted}
	task := model.Task{ID: "t1", DependsOn: []string{"dep1"}}
	ready, fatal := state.DependencyGate(task, map[string]model.Task{"dep1": dep})
	if !ready || fatal {
		t.Error("all deps completed: should be ready")
	}
}

func TestDependencyGate_DepPending(t *testing.T) {
	dep := model.Task{ID: "dep1", Status: model.TaskStatusRunning}
	task := model.Task{ID: "t1", DependsOn: []string{"dep1"}}
	ready, fatal := state.DependencyGate(task, map[string]model.Task{"dep1": dep})
	if ready || fatal {
		t.Error("pending dep: should not be ready and non-fatal")
	}
}

func TestDependencyGate_DepFailedWithFailPolicy(t *testing.T) {
	dep := model.Task{ID: "dep1", Status: model.TaskStatusFailed}
	task := model.Task{ID: "t1", DependsOn: []string{"dep1"}, OnDependencyFailed: model.OnDependencyFailedFail}
	ready, fatal := state.DependencyGate(task, map[string]model.Task{"dep1": dep})
	if ready || !fatal {
		t.Error("failed dep with Fail policy: should not be ready and be fatal")
	}
}

func TestDependencyGate_DepFailedWithSkipPolicy(t *testing.T) {
	dep := model.Task{ID: "dep1", Status: model.TaskStatusFailed}
	task := model.Task{ID: "t1", DependsOn: []string{"dep1"}, OnDependencyFailed: model.OnDependencyFailedSkip}
	ready, fatal := state.DependencyGate(task, map[string]model.Task{"dep1": dep})
	if !ready || fatal {
		t.Error("failed dep with Skip policy: should be ready and non-fatal")
	}
}

func TestDependencyGate_AwaitModeAny(t *testing.T) {
	dep1 := model.Task{ID: "dep1", Status: model.TaskStatusCompleted}
	dep2 := model.Task{ID: "dep2", Status: model.TaskStatusRunning}
	task := model.Task{
		ID:        "t1",
		DependsOn: []string{"dep1", "dep2"},
		AwaitMode: model.AwaitModeAny,
	}
	ready, fatal := state.DependencyGate(task, map[string]model.Task{"dep1": dep1, "dep2": dep2})
	if !ready || fatal {
		t.Error("AwaitModeAny with one completed dep: should be ready")
	}
}

func TestDependencyGate_AwaitModeQuorum(t *testing.T) {
	tasks := map[string]model.Task{
		"d1": {ID: "d1", Status: model.TaskStatusCompleted},
		"d2": {ID: "d2", Status: model.TaskStatusCompleted},
		"d3": {ID: "d3", Status: model.TaskStatusRunning},
	}
	task := model.Task{
		ID:          "t1",
		DependsOn:   []string{"d1", "d2", "d3"},
		AwaitMode:   model.AwaitModeQuorum,
		AwaitQuorum: 2,
	}
	ready, fatal := state.DependencyGate(task, tasks)
	if !ready || fatal {
		t.Error("AwaitModeQuorum with quorum=2 and 2 completed: should be ready")
	}
}
