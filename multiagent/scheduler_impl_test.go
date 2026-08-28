package multiagent

import (
	"context"
	"testing"

	"github.com/Viking602/venat/api"
)

func finishedInstance(class, runID string) AgentInstance {
	taskID := runID + "-" + class
	return AgentInstance{
		ID:        ComputeInstanceID(class, runID, taskID, "0"),
		ClassName: class,
		RunID:     runID,
		TaskID:    taskID,
		State:     InstanceStateFinished,
	}
}

func reportTask(class, runID string, structured map[string]any) api.Task {
	return api.Task{
		ID:     runID + "-" + class,
		RunID:  runID,
		Status: api.TaskStatusCompleted,
		Result: &api.TypedReport{Status: api.ReportStatusSuccess, Structured: structured},
	}
}

func TestSequentialSchedulerAdvancesInOrder(t *testing.T) {
	classes := []AgentClass{{Name: "research"}, {Name: "write"}, {Name: "review"}}
	scheduler := SequentialScheduler{Classes: classes}
	ctx := context.Background()

	// Empty state: dispatch the first class.
	got, err := scheduler.Next(ctx, TeamState{RunID: "run-1"})
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if len(got) != 1 || got[0].Task.RunID != "run-1" || got[0].Task.Goal != "" || got[0].Task.ID != "run-1-research" {
		t.Fatalf("first dispatch = %#v", got)
	}

	// First finished: dispatch the second class with the first report as input.
	state := TeamState{
		RunID:     "run-1",
		Instances: []AgentInstance{finishedInstance("research", "run-1")},
		Tasks:     []api.Task{reportTask("research", "run-1", map[string]any{"k": "v"})},
	}
	got, err = scheduler.Next(ctx, state)
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if len(got) != 1 || got[0].Task.ID != "run-1-write" {
		t.Fatalf("second dispatch = %#v", got)
	}
	if len(got[0].Input) == 0 {
		t.Fatal("second dispatch should thread the first report as input")
	}

	// All finished: terminal (no dispatch).
	state.Instances = []AgentInstance{
		finishedInstance("research", "run-1"),
		finishedInstance("write", "run-1"),
		finishedInstance("review", "run-1"),
	}
	got, err = scheduler.Next(ctx, state)
	if err != nil || len(got) != 0 {
		t.Fatalf("terminal Next() = %#v, err = %v", got, err)
	}
}

func TestSequentialSchedulerWaitsWhileActiveAndStopsOnFailure(t *testing.T) {
	scheduler := SequentialScheduler{Classes: []AgentClass{{Name: "a"}, {Name: "b"}}}
	ctx := context.Background()

	active := TeamState{RunID: "r", Instances: []AgentInstance{{ClassName: "a", State: InstanceStateRunning}}}
	if got, _ := scheduler.Next(ctx, active); len(got) != 0 {
		t.Fatalf("should wait while an instance is active, got %#v", got)
	}
	failed := TeamState{RunID: "r", Instances: []AgentInstance{{ClassName: "a", State: InstanceStateFailed}}}
	if got, _ := scheduler.Next(ctx, failed); len(got) != 0 {
		t.Fatalf("should terminate on failure, got %#v", got)
	}
}
