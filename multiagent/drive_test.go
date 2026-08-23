package multiagent

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Viking602/venat/api"
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

func TestDrivePersistsDistinctAgentClassIdentity(t *testing.T) {
	scheduler := SchedulerFunc(func(_ context.Context, state TeamState) ([]Dispatch, error) {
		if len(state.Instances) > 0 {
			return nil, nil
		}
		return []Dispatch{{
			To:             "instance-1",
			ClassName:      "draft-slot",
			AgentClassName: "writer",
			Task: api.Task{
				ID:    "run-1-draft",
				RunID: "run-1",
			},
		}}, nil
	})
	result, err := Drive(context.Background(), "run-1", scheduler, ExecutorFunc(
		func(context.Context, Dispatch) (api.TypedReport, error) {
			return api.TypedReport{Status: api.ReportStatusSuccess}, nil
		},
	), DriveOptions{})
	if err != nil {
		t.Fatalf("Drive error = %v", err)
	}
	if len(result.State.Instances) != 1 {
		t.Fatalf("instances = %#v, want one", result.State.Instances)
	}
	instance := result.State.Instances[0]
	if instance.ClassName != "draft-slot" || instance.AgentClassName != "writer" {
		t.Fatalf("persisted instance identity = %#v", instance)
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

func TestDriveContainsExecutorPanicAsFailedInstance(t *testing.T) {
	scheduler := SequentialScheduler{Classes: []AgentClass{{Name: "a"}}}
	executor := ExecutorFunc(func(context.Context, Dispatch) (api.TypedReport, error) {
		panic("provider adapter bug")
	})
	result, err := Drive(context.Background(), "run-1", scheduler, executor, DriveOptions{})
	if !errors.Is(err, ErrExecutorPanic) {
		t.Fatalf("Drive error = %v, want ErrExecutorPanic", err)
	}
	if len(result.State.Instances) != 1 ||
		result.State.Instances[0].State != InstanceStateFailed ||
		result.State.Tasks[0].Status != api.TaskStatusFailed {
		t.Fatalf("panic result = %#v", result.State)
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

func TestDrive_PassesContextCancellationThroughUnwrapped(t *testing.T) {
	// A custom Scheduler surfacing cancellation from mid-Next work: Drive's
	// loop-top ctx check cannot catch this, so the wrap branch must skip it.
	scheduler := SchedulerFunc(func(context.Context, TeamState) ([]Dispatch, error) {
		return nil, context.Canceled
	})
	executor := ExecutorFunc(func(context.Context, Dispatch) (api.TypedReport, error) {
		return api.TypedReport{}, nil
	})

	_, err := Drive(context.Background(), "run-1", scheduler, executor, DriveOptions{})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Drive error = %v, want context.Canceled", err)
	}
	var failure *SchedulerFailureError
	if errors.As(err, &failure) {
		t.Fatalf("cancellation must not be wrapped as SchedulerFailureError, got %v", err)
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

func TestDriveSemaphoreAcquireRespectsCancel(t *testing.T) {
	var ran atomic.Int32
	held := make(chan struct{}, 2)
	hold := make(chan struct{})
	scheduler := SchedulerFunc(func(_ context.Context, state TeamState) ([]Dispatch, error) {
		if len(state.Instances) > 0 {
			return nil, nil
		}
		return []Dispatch{
			buildDispatch(state.RunID, AgentClass{Name: "a"}, 0, nil),
			buildDispatch(state.RunID, AgentClass{Name: "b"}, 1, nil),
			buildDispatch(state.RunID, AgentClass{Name: "c"}, 2, nil),
		}, nil
	})
	executor := ExecutorFunc(func(context.Context, Dispatch) (api.TypedReport, error) {
		ran.Add(1)
		held <- struct{}{}
		<-hold
		return api.TypedReport{Status: api.ReportStatusSuccess}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := Drive(ctx, "run-sem-cancel", scheduler, executor, DriveOptions{MaxConcurrency: 2})
		done <- err
	}()
	<-held
	<-held
	cancel()
	// The third acquire is parked on the full semaphore. Give the
	// cancelled select a chance to observe Done before slots free.
	time.Sleep(20 * time.Millisecond)
	close(hold)
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Drive() error = %v", err)
	}
	if got := ran.Load(); got != 2 {
		t.Fatalf("executed %d dispatches after cancel, want 2", got)
	}
}

func TestDriveZeroValueConcurrencyIsBounded(t *testing.T) {
	peak := drivePeakConcurrency(t, DriveOptions{}, 4)
	if peak != defaultMaxConcurrency {
		t.Fatalf("zero-value peak concurrency = %d, want %d", peak, defaultMaxConcurrency)
	}

	peak = drivePeakConcurrency(t, DriveOptions{UnlimitedConcurrency: true}, 6)
	if peak != 6 {
		t.Fatalf("unlimited peak concurrency = %d, want 6", peak)
	}
}

func drivePeakConcurrency(t *testing.T, opts DriveOptions, releaseAt int32) int32 {
	t.Helper()
	const dispatchCount = 6
	scheduler := SchedulerFunc(func(_ context.Context, state TeamState) ([]Dispatch, error) {
		if len(state.Instances) > 0 {
			return nil, nil
		}
		dispatches := make([]Dispatch, dispatchCount)
		for i := range dispatches {
			dispatches[i] = buildDispatch(state.RunID, AgentClass{Name: string(rune('a' + i))}, i, nil)
		}
		return dispatches, nil
	})

	var active, peak atomic.Int32
	release := make(chan struct{})
	var once sync.Once
	executor := ExecutorFunc(func(context.Context, Dispatch) (api.TypedReport, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for previous := peak.Load(); current > previous; previous = peak.Load() {
			if peak.CompareAndSwap(previous, current) {
				break
			}
		}
		if current == releaseAt {
			once.Do(func() { close(release) })
		}
		<-release
		return api.TypedReport{Status: api.ReportStatusSuccess}, nil
	})

	if _, err := Drive(context.Background(), "run-bounded", scheduler, executor, opts); err != nil {
		t.Fatalf("Drive() error = %v", err)
	}
	return peak.Load()
}

// TestSupervisorRetryObservesLatestDecision is the regression for the
// stale-decision bug: when SupervisorActionRetry re-dispatches the
// supervisor, the retried run produces a new report, but reportForClass
// used to return the FIRST finished instance (the original Retry
// decision), freezing the loop until MaxTicks. With the fix it returns
// the LATEST finished instance, so a retry that says Accept terminates.
func TestSupervisorRetryObservesLatestDecision(t *testing.T) {
	scheduler := SupervisorScheduler{
		Supervisor: AgentClass{Name: "boss"},
		Workers:    map[string]AgentClass{"writer": {Name: "writer"}},
	}
	// boss returns Retry on its first run, Accept on its second. Pre-fix
	// reportForClass kept reading the first run's Retry → ErrMaxTicksExceeded.
	calls := 0
	executor := ExecutorFunc(func(_ context.Context, dispatch Dispatch) (api.TypedReport, error) {
		if classNameFromTaskID("run-1", dispatch.Task.ID) != "boss" {
			return api.TypedReport{}, errors.New("unexpected non-boss dispatch")
		}
		calls++
		action := SupervisorActionRetry
		if calls >= 2 {
			action = SupervisorActionAccept
		}
		return api.TypedReport{
			Status:     api.ReportStatusSuccess,
			Structured: map[string]any{"action": string(action)},
		}, nil
	})

	result, err := Drive(context.Background(), "run-1", scheduler, executor, DriveOptions{MaxTicks: 8})
	if err != nil {
		t.Fatalf("Drive error = %v, want nil (retry then accept should terminate)", err)
	}
	if calls != 2 {
		t.Fatalf("boss dispatches = %d, want 2 (retry + accept)", calls)
	}
	if len(result.State.Instances) != 2 {
		t.Fatalf("instances = %d, want 2 (two boss runs)", len(result.State.Instances))
	}
}
