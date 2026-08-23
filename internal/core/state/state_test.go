package state_test

import (
	"errors"
	"testing"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/state"
)

// ── IsTerminalRun ────────────────────────────────────────────────────────────

func TestIsTerminalRun(t *testing.T) {
	terminal := []api.RunStatus{
		api.RunStatusCompleted,
		api.RunStatusFailed,
		api.RunStatusCancelled,
	}
	for _, s := range terminal {
		if !state.IsTerminalRun(s) {
			t.Errorf("expected %q to be terminal", s)
		}
	}
	nonTerminal := []api.RunStatus{
		api.RunStatusCreated,
		api.RunStatusRunning,
		api.RunStatusPlanning,
		api.RunStatusBlocked,
		api.RunStatusWaitingApproval,
	}
	for _, s := range nonTerminal {
		if state.IsTerminalRun(s) {
			t.Errorf("expected %q to be non-terminal", s)
		}
	}
}

// ── IsTerminalTask ───────────────────────────────────────────────────────────

func TestIsTerminalTask(t *testing.T) {
	terminal := []api.TaskStatus{
		api.TaskStatusCompleted,
		api.TaskStatusFailed,
		api.TaskStatusCancelled,
	}
	for _, s := range terminal {
		if !state.IsTerminalTask(s) {
			t.Errorf("expected %q to be terminal", s)
		}
	}
	nonTerminal := []api.TaskStatus{
		api.TaskStatusCreated,
		api.TaskStatusRunning,
		api.TaskStatusDispatched,
		api.TaskStatusPaused,
		api.TaskStatusBlocked,
	}
	for _, s := range nonTerminal {
		if state.IsTerminalTask(s) {
			t.Errorf("expected %q to be non-terminal", s)
		}
	}
}

// ── TaskCanBecomeReady ───────────────────────────────────────────────────────

func TestTaskCanBecomeReady(t *testing.T) {
	ready := []api.TaskStatus{
		api.TaskStatusCreated,
		api.TaskStatusPlanned,
		api.TaskStatusValidated,
		api.TaskStatusRouted,
		api.TaskStatusWaitingDependency,
	}
	for _, s := range ready {
		if !state.TaskCanBecomeReady(s) {
			t.Errorf("expected %q to be able to become ready", s)
		}
	}
	notReady := []api.TaskStatus{
		api.TaskStatusDispatched,
		api.TaskStatusRunning,
		api.TaskStatusCompleted,
		api.TaskStatusFailed,
		api.TaskStatusCancelled,
	}
	for _, s := range notReady {
		if state.TaskCanBecomeReady(s) {
			t.Errorf("expected %q to NOT be able to become ready", s)
		}
	}
}

// ── TransitionRun ────────────────────────────────────────────────────────────

func TestTransitionRun_ValidTransition(t *testing.T) {
	run := api.Run{ID: "r1", Status: api.RunStatusCreated}
	next, err := state.TransitionRun(run, api.RunStatusPlanning)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next.Status != api.RunStatusPlanning {
		t.Errorf("want Planning, got %q", next.Status)
	}
}

func TestTransitionRun_SameStatus(t *testing.T) {
	run := api.Run{ID: "r1", Status: api.RunStatusRunning}
	next, err := state.TransitionRun(run, api.RunStatusRunning)
	if err != nil {
		t.Fatalf("same-status transition should not error: %v", err)
	}
	if next.Status != api.RunStatusRunning {
		t.Errorf("status should remain Running")
	}
}

func TestTransitionRun_InvalidTransition(t *testing.T) {
	run := api.Run{ID: "r1", Status: api.RunStatusCompleted}
	_, err := state.TransitionRun(run, api.RunStatusRunning)
	if !errors.Is(err, api.ErrTerminalState) {
		t.Fatalf("want ErrTerminalState, got %v", err)
	}
}

func TestTransitionRun_ForbiddenTransition(t *testing.T) {
	run := api.Run{ID: "r1", Status: api.RunStatusCreated}
	_, err := state.TransitionRun(run, api.RunStatusCompleted)
	if !errors.Is(err, api.ErrInvalidTransition) {
		t.Fatalf("want ErrInvalidTransition, got %v", err)
	}
}

// ── TransitionTask ───────────────────────────────────────────────────────────

func TestTransitionTask_ValidTransition(t *testing.T) {
	task := api.Task{ID: "t1", Status: api.TaskStatusDispatched, Version: 1}
	next, err := state.TransitionTask(task, api.TaskStatusRunning, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next.Status != api.TaskStatusRunning {
		t.Errorf("want Running, got %q", next.Status)
	}
	if next.Version != 2 {
		t.Errorf("version should be bumped to 2, got %d", next.Version)
	}
}

func TestTransitionTask_NoBumpVersion(t *testing.T) {
	task := api.Task{ID: "t1", Status: api.TaskStatusDispatched, Version: 5}
	next, err := state.TransitionTask(task, api.TaskStatusRunning, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next.Version != 5 {
		t.Errorf("version should remain 5, got %d", next.Version)
	}
}

func TestTransitionTask_TerminalBlocked(t *testing.T) {
	task := api.Task{ID: "t1", Status: api.TaskStatusCompleted}
	_, err := state.TransitionTask(task, api.TaskStatusRunning, false)
	if !errors.Is(err, api.ErrTerminalState) {
		t.Fatalf("want ErrTerminalState, got %v", err)
	}
}

func TestTransitionTask_ForbiddenTransition(t *testing.T) {
	task := api.Task{ID: "t1", Status: api.TaskStatusCreated}
	_, err := state.TransitionTask(task, api.TaskStatusCompleted, false)
	if !errors.Is(err, api.ErrInvalidTransition) {
		t.Fatalf("want ErrInvalidTransition, got %v", err)
	}
}

// ── DependencyGate ───────────────────────────────────────────────────────────

func TestDependencyGate_NoDeps(t *testing.T) {
	task := api.Task{ID: "t1"}
	ready, fatal := state.DependencyGate(task, nil)
	if !ready || fatal {
		t.Error("task with no deps should be ready and non-fatal")
	}
}

func TestDependencyGate_AllCompleted(t *testing.T) {
	dep := api.Task{ID: "dep1", Status: api.TaskStatusCompleted}
	task := api.Task{ID: "t1", DependsOn: []string{"dep1"}}
	ready, fatal := state.DependencyGate(task, map[string]api.Task{"dep1": dep})
	if !ready || fatal {
		t.Error("all deps completed: should be ready")
	}
}

func TestDependencyGate_DepPending(t *testing.T) {
	dep := api.Task{ID: "dep1", Status: api.TaskStatusRunning}
	task := api.Task{ID: "t1", DependsOn: []string{"dep1"}}
	ready, fatal := state.DependencyGate(task, map[string]api.Task{"dep1": dep})
	if ready || fatal {
		t.Error("pending dep: should not be ready and non-fatal")
	}
}

func TestDependencyGate_DepFailedWithFailPolicy(t *testing.T) {
	dep := api.Task{ID: "dep1", Status: api.TaskStatusFailed}
	task := api.Task{ID: "t1", DependsOn: []string{"dep1"}, OnDependencyFailed: api.OnDependencyFailedFail}
	ready, fatal := state.DependencyGate(task, map[string]api.Task{"dep1": dep})
	if ready || !fatal {
		t.Error("failed dep with Fail policy: should not be ready and be fatal")
	}
}

func TestDependencyGate_DepFailedWithSkipPolicy(t *testing.T) {
	dep := api.Task{ID: "dep1", Status: api.TaskStatusFailed}
	task := api.Task{ID: "t1", DependsOn: []string{"dep1"}, OnDependencyFailed: api.OnDependencyFailedSkip}
	ready, fatal := state.DependencyGate(task, map[string]api.Task{"dep1": dep})
	if !ready || fatal {
		t.Error("failed dep with Skip policy: should be ready and non-fatal")
	}
}

func TestDependencyGate_AwaitModeAny(t *testing.T) {
	dep1 := api.Task{ID: "dep1", Status: api.TaskStatusCompleted}
	dep2 := api.Task{ID: "dep2", Status: api.TaskStatusRunning}
	task := api.Task{
		ID:        "t1",
		DependsOn: []string{"dep1", "dep2"},
		AwaitMode: api.AwaitModeAny,
	}
	ready, fatal := state.DependencyGate(task, map[string]api.Task{"dep1": dep1, "dep2": dep2})
	if !ready || fatal {
		t.Error("AwaitModeAny with one completed dep: should be ready")
	}
}

func TestDependencyGate_AwaitModeQuorum(t *testing.T) {
	tasks := map[string]api.Task{
		"d1": {ID: "d1", Status: api.TaskStatusCompleted},
		"d2": {ID: "d2", Status: api.TaskStatusCompleted},
		"d3": {ID: "d3", Status: api.TaskStatusRunning},
	}
	task := api.Task{
		ID:          "t1",
		DependsOn:   []string{"d1", "d2", "d3"},
		AwaitMode:   api.AwaitModeQuorum,
		AwaitQuorum: 2,
	}
	ready, fatal := state.DependencyGate(task, tasks)
	if !ready || fatal {
		t.Error("AwaitModeQuorum with quorum=2 and 2 completed: should be ready")
	}
}
