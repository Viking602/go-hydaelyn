package multiagent

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
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

func TestRouterSchedulerBranchesOnDiscriminator(t *testing.T) {
	entry := AgentClass{Name: "triage"}
	scheduler := RouterScheduler{
		Entry:              entry,
		DiscriminatorField: "severity",
		Routes: map[string]AgentClass{
			"high": {Name: "pager"},
			"low":  {Name: "ticket"},
		},
	}
	ctx := context.Background()

	// No entry yet: dispatch entry.
	if got, _ := scheduler.Next(ctx, TeamState{RunID: "run-1"}); len(got) != 1 || got[0].Task.ID != "run-1-triage" {
		t.Fatalf("entry dispatch = %#v", got)
	}

	for value, wantClass := range map[string]string{"high": "pager", "low": "ticket"} {
		state := TeamState{
			RunID:     "run-1",
			Instances: []AgentInstance{finishedInstance("triage", "run-1")},
			Tasks:     []api.Task{reportTask("triage", "run-1", map[string]any{"severity": value})},
		}
		got, err := scheduler.Next(ctx, state)
		if err != nil {
			t.Fatalf("Next(%s) error = %v", value, err)
		}
		if len(got) != 1 || got[0].Task.ID != "run-1-"+wantClass {
			t.Fatalf("route %q dispatched %#v, want %s", value, got, wantClass)
		}
	}
}

func TestRouterSchedulerErrorsWithoutRouteOrDefault(t *testing.T) {
	scheduler := RouterScheduler{
		Entry:              AgentClass{Name: "triage"},
		DiscriminatorField: "severity",
		Routes:             map[string]AgentClass{"high": {Name: "pager"}},
	}
	state := TeamState{
		RunID:     "run-1",
		Instances: []AgentInstance{finishedInstance("triage", "run-1")},
		Tasks:     []api.Task{reportTask("triage", "run-1", map[string]any{"severity": "unknown"})},
	}
	if _, err := scheduler.Next(context.Background(), state); err == nil {
		t.Fatal("expected error when no route matches and no default is set")
	}
}

func TestRouterSchedulerUsesDefault(t *testing.T) {
	fallback := AgentClass{Name: "ticket"}
	scheduler := RouterScheduler{
		Entry:              AgentClass{Name: "triage"},
		DiscriminatorField: "severity",
		Routes:             map[string]AgentClass{"high": {Name: "pager"}},
		Default:            &fallback,
	}
	state := TeamState{
		RunID:     "run-1",
		Instances: []AgentInstance{finishedInstance("triage", "run-1")},
		Tasks:     []api.Task{reportTask("triage", "run-1", map[string]any{"severity": "meh"})},
	}
	got, err := scheduler.Next(context.Background(), state)
	if err != nil || len(got) != 1 || got[0].Task.ID != "run-1-ticket" {
		t.Fatalf("default route dispatched %#v, err = %v", got, err)
	}
}

func TestSupervisorSchedulerExecutesHandoffDecision(t *testing.T) {
	scheduler := SupervisorScheduler{
		Supervisor: AgentClass{Name: "boss"},
		Workers:    map[string]AgentClass{"writer": {Name: "writer"}},
	}
	ctx := context.Background()

	// No supervisor yet: dispatch the supervisor.
	if got, _ := scheduler.Next(ctx, TeamState{RunID: "run-1"}); len(got) != 1 || got[0].Task.ID != "run-1-boss" {
		t.Fatalf("supervisor dispatch = %#v", got)
	}

	// Supervisor finished with a handoff decision: dispatch the worker.
	state := TeamState{
		RunID:     "run-1",
		Instances: []AgentInstance{finishedInstance("boss", "run-1")},
		Tasks: []api.Task{reportTask("boss", "run-1", map[string]any{
			"action":    string(SupervisorActionHandoff),
			"handoffTo": "writer",
		})},
	}
	got, err := scheduler.Next(ctx, state)
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if len(got) != 1 || got[0].Task.ID != "run-1-writer" {
		t.Fatalf("handoff dispatched %#v, want writer", got)
	}
}

func TestSupervisorSchedulerAcceptTerminatesAndUnknownTargetErrors(t *testing.T) {
	scheduler := SupervisorScheduler{Supervisor: AgentClass{Name: "boss"}, Workers: map[string]AgentClass{}}
	ctx := context.Background()

	accept := TeamState{
		RunID:     "run-1",
		Instances: []AgentInstance{finishedInstance("boss", "run-1")},
		Tasks:     []api.Task{reportTask("boss", "run-1", map[string]any{"action": string(SupervisorActionAccept)})},
	}
	if got, err := scheduler.Next(ctx, accept); err != nil || len(got) != 0 {
		t.Fatalf("accept should terminate: got %#v, err %v", got, err)
	}

	bad := TeamState{
		RunID:     "run-1",
		Instances: []AgentInstance{finishedInstance("boss", "run-1")},
		Tasks:     []api.Task{reportTask("boss", "run-1", map[string]any{"action": string(SupervisorActionHandoff), "handoffTo": "ghost"})},
	}
	if _, err := scheduler.Next(ctx, bad); err == nil {
		t.Fatal("expected error for unknown handoff target")
	}
}
