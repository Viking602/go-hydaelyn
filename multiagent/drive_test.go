package multiagent

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
)

// reportExecutor returns a fixed Structured payload per class name, letting
// tests drive Router/Supervisor branches deterministically.
func reportExecutor(runID string, byClass map[string]map[string]any) Executor {
	return ExecutorFunc(func(_ context.Context, dispatch Dispatch) (api.TypedReport, error) {
		class := classNameFromTaskID(runID, dispatch.Task.ID)
		return api.TypedReport{Status: api.ReportStatusSuccess, Structured: byClass[class]}, nil
	})
}

func classNames(state TeamState) []string {
	out := make([]string, 0, len(state.Instances))
	for _, instance := range state.Instances {
		out = append(out, instance.ClassName)
	}
	return out
}

func TestDriveRunsSequentialToCompletion(t *testing.T) {
	scheduler := SequentialScheduler{Classes: []AgentClass{{Name: "research"}, {Name: "write"}, {Name: "review"}}}
	executor := reportExecutor("run-1", map[string]map[string]any{})

	result, err := Drive(context.Background(), "run-1", scheduler, executor, DriveOptions{})
	if err != nil {
		t.Fatalf("Drive error = %v", err)
	}
	got := classNames(result.State)
	want := []string{"research", "write", "review"}
	if len(got) != len(want) {
		t.Fatalf("executed classes = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("class[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if result.Ticks != 3 {
		t.Fatalf("ticks = %d, want 3", result.Ticks)
	}
}

func TestDriveRoutesThenTerminates(t *testing.T) {
	scheduler := RouterScheduler{
		Entry:              AgentClass{Name: "triage"},
		DiscriminatorField: "severity",
		Routes:             map[string]AgentClass{"high": {Name: "pager"}},
	}
	executor := reportExecutor("run-1", map[string]map[string]any{
		"triage": {"severity": "high"},
	})

	result, err := Drive(context.Background(), "run-1", scheduler, executor, DriveOptions{})
	if err != nil {
		t.Fatalf("Drive error = %v", err)
	}
	got := classNames(result.State)
	if len(got) != 2 || got[0] != "triage" || got[1] != "pager" {
		t.Fatalf("routed classes = %#v, want [triage pager]", got)
	}
}

func TestDriveSupervisorHandoffThenAccept(t *testing.T) {
	scheduler := SupervisorScheduler{
		Supervisor: AgentClass{Name: "boss"},
		Workers:    map[string]AgentClass{"writer": {Name: "writer"}},
	}
	// boss hands off to writer; writer's report carries no decision, so the
	// next supervisor tick sees the worker finished and the boss already
	// finished — terminal.
	executor := reportExecutor("run-1", map[string]map[string]any{
		"boss": {"action": string(SupervisorActionHandoff), "handoffTo": "writer"},
	})

	result, err := Drive(context.Background(), "run-1", scheduler, executor, DriveOptions{})
	if err != nil {
		t.Fatalf("Drive error = %v", err)
	}
	got := classNames(result.State)
	if len(got) != 2 || got[0] != "boss" || got[1] != "writer" {
		t.Fatalf("supervisor classes = %#v, want [boss writer]", got)
	}
}

func TestDriveSurfacesExecutorErrorAsFailedInstance(t *testing.T) {
	scheduler := SequentialScheduler{Classes: []AgentClass{{Name: "a"}, {Name: "b"}}}
	boom := errors.New("executor boom")
	executor := ExecutorFunc(func(_ context.Context, _ Dispatch) (api.TypedReport, error) {
		return api.TypedReport{}, boom
	})

	result, err := Drive(context.Background(), "run-1", scheduler, executor, DriveOptions{})
	if !errors.Is(err, boom) {
		t.Fatalf("Drive error = %v, want executor boom", err)
	}
	if len(result.State.Instances) != 1 || result.State.Instances[0].State != InstanceStateFailed {
		t.Fatalf("expected one failed instance, got %#v", result.State.Instances)
	}
}

func TestDrive_WrapsSchedulerErrorAsSchedulerFailure(t *testing.T) {
	boom := errors.New("no agent can handle current state")
	scheduler := SchedulerFunc(func(context.Context, TeamState) ([]Dispatch, error) {
		return nil, boom
	})
	executor := ExecutorFunc(func(context.Context, Dispatch) (api.TypedReport, error) {
		return api.TypedReport{}, nil
	})

	_, err := Drive(context.Background(), "run-1", scheduler, executor, DriveOptions{})

	var failure *SchedulerFailureError
	if !errors.As(err, &failure) {
		t.Fatalf("errors.As(err, *SchedulerFailureError) = false, err = %v", err)
	}
	if failure.RunID != "run-1" || failure.Tick != 1 {
		t.Fatalf("unexpected failure fields: %+v", failure)
	}
	if !errors.Is(err, boom) {
		t.Fatal("wrapped error must still satisfy errors.Is on the cause")
	}
}

func TestDriveStopsAtMaxTicks(t *testing.T) {
	// A scheduler that always dispatches a fresh class never terminates.
	endless := SchedulerFunc(func(_ context.Context, state TeamState) ([]Dispatch, error) {
		return []Dispatch{buildDispatch(state.RunID, AgentClass{Name: "x"}, len(state.Instances), nil)}, nil
	})
	executor := reportExecutor("run-1", map[string]map[string]any{})
	_, err := Drive(context.Background(), "run-1", endless, executor, DriveOptions{MaxTicks: 3})
	if !errors.Is(err, ErrMaxTicksExceeded) {
		t.Fatalf("Drive error = %v, want ErrMaxTicksExceeded", err)
	}
}
