package host

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/scheduler"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

func newSemaphoreTestRuntime(t *testing.T) *Runtime {
	t.Helper()

	runner := New(Config{Storage: storage.NewMemoryDriver(), WorkerID: "worker-a"})
	runner.RegisterProvider("team-fake", teamFakeProvider{})
	runner.RegisterPattern(singleTaskPattern{})
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "team-fake", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "team-fake", Model: "test", MaxConcurrency: 1})
	return runner
}

func newSemaphoreTestState(t *testing.T) team.RunState {
	t.Helper()

	state, err := singleTaskPattern{}.Start(context.Background(), team.StartRequest{
		TeamID:            "team-semaphore",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input:             map[string]any{"task": "queued semaphore"},
	})
	if err != nil {
		t.Fatalf("singleTaskPattern.Start() error = %v", err)
	}
	state.Normalize()
	return state
}

func newPreparedLeaseForSemaphoreTest(t *testing.T) (*Runtime, team.RunState, preparedLease, *scheduler.MemoryQueue, map[string]chan struct{}) {
	t.Helper()

	runner := newSemaphoreTestRuntime(t)
	current := newSemaphoreTestState(t)
	queue := scheduler.NewMemoryQueue()
	runner.queue = queue
	runner.syncLeaseReleaser()

	task := current.Tasks[0]
	if err := queue.Enqueue(context.Background(), scheduler.TaskLease{
		TaskID:      task.ID,
		TeamID:      current.ID,
		TaskVersion: task.Version,
	}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	lease, ok, err := queue.Acquire(context.Background(), runner.workerID, localQueueLeaseTTL)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if !ok {
		t.Fatal("expected queued lease to be acquired")
	}
	agentInstance, profile, err := runner.resolveTaskExecution(current, task)
	if err != nil {
		t.Fatalf("resolveTaskExecution() error = %v", err)
	}
	semByProfile := map[string]chan struct{}{
		profile.Name: make(chan struct{}, profile.MaxConcurrency),
	}
	return runner, current, preparedLease{
		lease:         lease,
		index:         0,
		original:      task,
		agentInstance: agentInstance,
		profile:       profile,
	}, queue, semByProfile
}

func TestRunPreparedLeaseKeepsLeaseAliveWhileWaitingForSemaphore(t *testing.T) {
	runner, current, prepared, queue, semByProfile := newPreparedLeaseForSemaphoreTest(t)
	semByProfile[prepared.profile.Name] <- struct{}{}

	done := make(chan taskOutcome, 1)
	go func() {
		done <- runner.runPreparedLease(context.Background(), current, prepared, semByProfile)
	}()

	time.Sleep(localQueueLeaseTTL + 20*time.Millisecond)
	if err := queue.RecoverExpired(context.Background(), time.Now()); err != nil {
		t.Fatalf("RecoverExpired() error = %v", err)
	}
	duplicate, ok, err := queue.Acquire(context.Background(), "worker-b", time.Minute)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if ok {
		_ = queue.Release(context.Background(), duplicate)
		<-semByProfile[prepared.profile.Name]
		<-done
		t.Fatalf("expected waiting lease to remain live, got duplicate %#v", duplicate)
	}

	<-semByProfile[prepared.profile.Name]
	outcome := <-done
	if outcome.err != nil {
		t.Fatalf("expected queued task to complete after semaphore release, got %#v", outcome)
	}
}

func TestRunPreparedLeaseHonorsCancellationWhileWaitingForSemaphore(t *testing.T) {
	runner, current, prepared, queue, semByProfile := newPreparedLeaseForSemaphoreTest(t)
	semByProfile[prepared.profile.Name] <- struct{}{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan taskOutcome, 1)
	go func() {
		done <- runner.runPreparedLease(ctx, current, prepared, semByProfile)
	}()

	select {
	case outcome := <-done:
		if !errors.Is(outcome.err, context.Canceled) {
			t.Fatalf("expected context cancellation, got %#v", outcome)
		}
		if outcome.task.Status != team.TaskStatusAborted {
			t.Fatalf("expected aborted task after cancellation, got %#v", outcome.task)
		}
	case <-time.After(75 * time.Millisecond):
		<-semByProfile[prepared.profile.Name]
		outcome := <-done
		t.Fatalf("runPreparedLease() blocked on semaphore after cancellation, late outcome=%#v", outcome)
	}

	if got := queue.InflightCount(); got != 0 {
		t.Fatalf("expected lease release after cancellation, got inflight=%d", got)
	}
}

func TestExecuteRunnableTasksHonorsCancellationWhileWaitingForSemaphore(t *testing.T) {
	runner := newSemaphoreTestRuntime(t)
	current := newSemaphoreTestState(t)
	task := current.Tasks[0]

	_, profile, err := runner.resolveTaskExecution(current, task)
	if err != nil {
		t.Fatalf("resolveTaskExecution() error = %v", err)
	}
	semByProfile := map[string]chan struct{}{
		profile.Name: make(chan struct{}, profile.MaxConcurrency),
	}
	semByProfile[profile.Name] <- struct{}{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	outcomes := runner.executeRunnableTasks(ctx, current, map[string]struct{}{task.ID: {}}, semByProfile)
	select {
	case outcome, ok := <-outcomes:
		if !ok {
			t.Fatal("expected cancellation outcome")
		}
		if !errors.Is(outcome.err, context.Canceled) {
			t.Fatalf("expected context cancellation, got %#v", outcome)
		}
		if outcome.task.Status != team.TaskStatusAborted {
			t.Fatalf("expected aborted task after cancellation, got %#v", outcome.task)
		}
	case <-time.After(75 * time.Millisecond):
		<-semByProfile[profile.Name]
		outcome := <-outcomes
		t.Fatalf("executeRunnableTasks() blocked on semaphore after cancellation, late outcome=%#v", outcome)
	}
}

func TestDriveTeamReturnsSaveErrorWhenMaxStepsPersistenceFails(t *testing.T) {
	errSaveFailed := errors.New("save failed")
	baseDriver := storage.NewMemoryDriver()
	runner := New(Config{
		Storage: &queuedLeaseDriver{
			inner: baseDriver,
			teams: &queuedLeaseTeamStore{inner: baseDriver.Teams(), saveCASErr: errSaveFailed},
		},
		MaxTeamDriveSteps: 1,
	})
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor})

	state := team.RunState{
		ID:      "team-save-failure-limit",
		Pattern: "counting",
		Status:  team.StatusRunning,
		Phase:   team.PhaseResearch,
		Supervisor: team.AgentInstance{
			ID:          "supervisor",
			Role:        team.RoleSupervisor,
			ProfileName: "supervisor",
		},
	}
	state.Normalize()
	if err := baseDriver.Teams().Save(context.Background(), state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	state, _ = baseDriver.Teams().Load(context.Background(), state.ID)

	_, err := runner.driveTeam(context.Background(), countingPattern{calls: new(int)}, state)
	if !errors.Is(err, errSaveFailed) {
		t.Fatalf("expected save error %v, got %v", errSaveFailed, err)
	}
}
