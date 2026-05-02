package core

import (
	"context"
	"errors"
	"testing"
)

// completeTask is a tiny helper that walks a task to TaskStatusCompleted via
// the public lifecycle so the gate test fixtures are realistic.
//
// Convention: ctx is the first parameter (revive: context-as-argument).
// mustLoadTask still takes (t, ctx, ...) because it lives in refactor_test.go
// which is being touched by an in-flight refactor; its signature will be
// updated in a follow-up PR.
func completeTask(ctx context.Context, t *testing.T, rt *Runtime, runID, taskID string) {
	t.Helper()
	task := mustLoadTask(t, ctx, rt, runID, taskID)
	if _, err := mustExecuteWalkToCompleted(ctx, t, rt, task); err != nil {
		t.Fatalf("complete %s: %v", taskID, err)
	}
}

func mustExecuteWalkToCompleted(ctx context.Context, t *testing.T, rt *Runtime, task Task) (Task, error) {
	t.Helper()
	if err := rt.TransitionTask(ctx, TransitionTaskCommand{RunID: task.RunID, TaskID: task.ID, To: TaskStatusDispatched}); err != nil {
		return task, err
	}
	if err := rt.TransitionTask(ctx, TransitionTaskCommand{RunID: task.RunID, TaskID: task.ID, To: TaskStatusRunning}); err != nil {
		return task, err
	}
	if err := rt.TransitionTask(ctx, TransitionTaskCommand{RunID: task.RunID, TaskID: task.ID, To: TaskStatusCompleted}); err != nil {
		return task, err
	}
	return mustLoadTask(t, ctx, rt, task.RunID, task.ID), nil
}

func failTask(ctx context.Context, t *testing.T, rt *Runtime, runID, taskID string) {
	t.Helper()
	if err := rt.TransitionTask(ctx, TransitionTaskCommand{RunID: runID, TaskID: taskID, To: TaskStatusDispatched}); err != nil {
		t.Fatalf("dispatch dep: %v", err)
	}
	if err := rt.TransitionTask(ctx, TransitionTaskCommand{RunID: runID, TaskID: taskID, To: TaskStatusRunning}); err != nil {
		t.Fatalf("run dep: %v", err)
	}
	if err := rt.TransitionTask(ctx, TransitionTaskCommand{RunID: runID, TaskID: taskID, To: TaskStatusFailed}); err != nil {
		t.Fatalf("fail dep: %v", err)
	}
}

func TestDependencyGateAwaitModeAny(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(t, ctx, rt, "run-any")
	dep1 := mustCreateTask(t, ctx, rt, CreateTaskCommand{RunID: run.ID, TaskID: "d1", OwnerAgentID: "a"})
	mustCreateTask(t, ctx, rt, CreateTaskCommand{RunID: run.ID, TaskID: "d2", OwnerAgentID: "a"})
	child := mustCreateTask(t, ctx, rt, CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "child",
		OwnerAgentID: "a",
		DependsOn:    []string{"d1", "d2"},
		AwaitMode:    AwaitModeAny,
	})

	if _, err := rt.DispatchTask(ctx, DispatchTaskCommand{RunID: run.ID, TaskID: child.ID, TargetAgentID: "a"}); !errors.Is(err, ErrDependencyUnmet) {
		t.Fatalf("expected ErrDependencyUnmet before any dep completes, got %v", err)
	}
	completeTask(ctx, t, rt, run.ID, dep1.ID)
	if _, err := rt.DispatchTask(ctx, DispatchTaskCommand{RunID: run.ID, TaskID: child.ID, TargetAgentID: "a"}); err != nil {
		t.Fatalf("AwaitModeAny should release after one completion, got %v", err)
	}
}

func TestDependencyGateAwaitModeQuorum(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(t, ctx, rt, "run-quorum")
	d1 := mustCreateTask(t, ctx, rt, CreateTaskCommand{RunID: run.ID, TaskID: "d1", OwnerAgentID: "a"})
	d2 := mustCreateTask(t, ctx, rt, CreateTaskCommand{RunID: run.ID, TaskID: "d2", OwnerAgentID: "a"})
	mustCreateTask(t, ctx, rt, CreateTaskCommand{RunID: run.ID, TaskID: "d3", OwnerAgentID: "a"})
	child := mustCreateTask(t, ctx, rt, CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "child",
		OwnerAgentID: "a",
		DependsOn:    []string{"d1", "d2", "d3"},
		AwaitMode:    AwaitModeQuorum,
		AwaitQuorum:  2,
	})

	completeTask(ctx, t, rt, run.ID, d1.ID)
	if _, err := rt.DispatchTask(ctx, DispatchTaskCommand{RunID: run.ID, TaskID: child.ID, TargetAgentID: "a"}); !errors.Is(err, ErrDependencyUnmet) {
		t.Fatalf("expected ErrDependencyUnmet at 1/2 quorum, got %v", err)
	}
	completeTask(ctx, t, rt, run.ID, d2.ID)
	if _, err := rt.DispatchTask(ctx, DispatchTaskCommand{RunID: run.ID, TaskID: child.ID, TargetAgentID: "a"}); err != nil {
		t.Fatalf("quorum 2/3 should release, got %v", err)
	}
}

func TestDependencyGateOnDependencyFailedFail(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(t, ctx, rt, "run-fail")
	mustCreateTask(t, ctx, rt, CreateTaskCommand{RunID: run.ID, TaskID: "d1", OwnerAgentID: "a"})
	child := mustCreateTask(t, ctx, rt, CreateTaskCommand{
		RunID:              run.ID,
		TaskID:             "child",
		OwnerAgentID:       "a",
		DependsOn:          []string{"d1"},
		OnDependencyFailed: OnDependencyFailedFail,
	})

	failTask(ctx, t, rt, run.ID, "d1")
	if _, err := rt.DispatchTask(ctx, DispatchTaskCommand{RunID: run.ID, TaskID: child.ID, TargetAgentID: "a"}); !errors.Is(err, ErrDependencyFailed) {
		t.Fatalf("expected ErrDependencyFailed, got %v", err)
	}
}

func TestDependencyGateOnDependencyFailedSkip(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(t, ctx, rt, "run-skip")
	mustCreateTask(t, ctx, rt, CreateTaskCommand{RunID: run.ID, TaskID: "d1", OwnerAgentID: "a"})
	d2 := mustCreateTask(t, ctx, rt, CreateTaskCommand{RunID: run.ID, TaskID: "d2", OwnerAgentID: "a"})
	child := mustCreateTask(t, ctx, rt, CreateTaskCommand{
		RunID:              run.ID,
		TaskID:             "child",
		OwnerAgentID:       "a",
		DependsOn:          []string{"d1", "d2"},
		OnDependencyFailed: OnDependencyFailedSkip,
	})

	failTask(ctx, t, rt, run.ID, "d1")
	completeTask(ctx, t, rt, run.ID, d2.ID)
	if _, err := rt.DispatchTask(ctx, DispatchTaskCommand{RunID: run.ID, TaskID: child.ID, TargetAgentID: "a"}); err != nil {
		t.Fatalf("Skip should treat failed dep as completed (1 fail + 1 ok = 2/2), got %v", err)
	}
}

func TestDependencyGateBackwardCompatAllMode(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(t, ctx, rt, "run-all")
	d1 := mustCreateTask(t, ctx, rt, CreateTaskCommand{RunID: run.ID, TaskID: "d1", OwnerAgentID: "a"})
	d2 := mustCreateTask(t, ctx, rt, CreateTaskCommand{RunID: run.ID, TaskID: "d2", OwnerAgentID: "a"})
	child := mustCreateTask(t, ctx, rt, CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "child",
		OwnerAgentID: "a",
		DependsOn:    []string{"d1", "d2"},
	})

	completeTask(ctx, t, rt, run.ID, d1.ID)
	if _, err := rt.DispatchTask(ctx, DispatchTaskCommand{RunID: run.ID, TaskID: child.ID, TargetAgentID: "a"}); !errors.Is(err, ErrDependencyUnmet) {
		t.Fatalf("default AwaitModeAll should require both deps, got %v", err)
	}
	completeTask(ctx, t, rt, run.ID, d2.ID)
	if _, err := rt.DispatchTask(ctx, DispatchTaskCommand{RunID: run.ID, TaskID: child.ID, TargetAgentID: "a"}); err != nil {
		t.Fatalf("both deps complete should release, got %v", err)
	}
}
