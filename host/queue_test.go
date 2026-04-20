package host

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/blackboard"
	"github.com/Viking602/go-hydaelyn/plugin"
	"github.com/Viking602/go-hydaelyn/scheduler"
	"github.com/Viking602/go-hydaelyn/session"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

func TestQueuedStatePersistRejectsStaleVersion(t *testing.T) {
	driver := storage.NewMemoryDriver()
	runner := New(Config{Storage: driver})
	runner.RegisterPattern(linearPattern{})

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
	err = runner.persistQueuedTaskState(context.Background(), stale, team.Task{})
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
	runner := New(Config{WorkerID: "worker-a"})
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
	state = runner.applyQueuedTaskResult(context.Background(), state, 0,first)
	completedAt := state.Tasks[0].CompletedAt
	completedBy := state.Tasks[0].CompletedBy
	if completedAt.IsZero() || completedBy != "worker-a" {
		t.Fatalf("expected authoritative completion metadata, got %#v", state.Tasks[0])
	}
	duplicate := state.Tasks[0]
	duplicate.Result = &team.Result{Summary: "duplicate result must be ignored"}
	state = runner.applyQueuedTaskResult(context.Background(), state, 0,duplicate)
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

func TestResolveQueuedCommitConflictUsesStateVersionConflictReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		staleState    team.RunState
		mutateCurrent func(team.RunState) team.RunState
	}{
		{
			name: "current task terminal",
			staleState: team.RunState{
				ID:      "team-terminal-conflict",
				Pattern: "linear",
				Status:  team.StatusCompleted,
				Phase:   team.PhaseComplete,
				Tasks: []team.Task{{
					ID:     "task-1",
					Kind:   team.TaskKindResearch,
					Status: team.TaskStatusCompleted,
				}},
			},
			mutateCurrent: func(current team.RunState) team.RunState {
				current.Metadata = map[string]string{"winner": "authoritative-terminal"}
				return current
			},
		},
		{
			name: "current task newer version",
			staleState: team.RunState{
				ID:      "team-newer-version-conflict",
				Pattern: "linear",
				Status:  team.StatusRunning,
				Phase:   team.PhaseResearch,
				Tasks: []team.Task{{
					ID:              "task-1",
					Kind:            team.TaskKindResearch,
					RequiredRole:    team.RoleResearcher,
					AssigneeAgentID: "worker-1",
					FailurePolicy:   team.FailurePolicyFailFast,
					Status:          team.TaskStatusPending,
				}},
			},
			mutateCurrent: func(current team.RunState) team.RunState {
				current.Tasks[0].Version++
				current.Metadata = map[string]string{"winner": "authoritative-newer-version"}
				return current
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			driver := storage.NewMemoryDriver()
			runner := New(Config{Storage: driver, WorkerID: "worker-a"})

			initial := tc.staleState
			initial.Normalize()
			if err := driver.Teams().Save(ctx, initial); err != nil {
				t.Fatalf("Save() error = %v", err)
			}

			stale, err := driver.Teams().Load(ctx, initial.ID)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			current := stale
			current.Tasks = append([]team.Task(nil), stale.Tasks...)
			current = tc.mutateCurrent(current)
			if _, err := driver.Teams().SaveCAS(ctx, current, stale.Version); err != nil {
				t.Fatalf("SaveCAS() error = %v", err)
			}

			commitErr := runner.resolveQueuedCommitConflict(ctx, stale.ID, stale.Tasks[0], storage.ErrStaleState)
			if !errors.Is(commitErr, errQueuedTaskAlreadyCommitted) {
				t.Fatalf("expected errQueuedTaskAlreadyCommitted, got %v", commitErr)
			}

			events, err := driver.Events().List(ctx, stale.ID)
			if err != nil {
				t.Fatalf("Events().List() error = %v", err)
			}

			assertQueuedConflictReason(t, events, stale.Tasks[0].ID)
		})
	}
}

func TestRunQueuedLeaseReleasesLeaseOnAllErrorPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("loadQueuedExecutionState error releases lease", func(t *testing.T) {
		errLoadFailed := errors.New("load failed")
		baseDriver := storage.NewMemoryDriver()
		queue := &queuedLeaseSpy{inner: scheduler.NewMemoryQueue()}
		runner := New(Config{
			Storage: &queuedLeaseDriver{
				inner: baseDriver,
				teams: &queuedLeaseTeamStore{inner: baseDriver.Teams(), loadErr: errLoadFailed},
			},
			WorkerID: "worker-a",
		})
		runner.queue = queue

		lease := queuedLeaseAcquire(t, ctx, queue, runner.workerID, scheduler.TaskLease{TeamID: "team-load", TaskID: "task-load"})
		err := runner.runQueuedLease(ctx, lease)
		if !errors.Is(err, errLoadFailed) {
			t.Fatalf("expected load error %v, got %v", errLoadFailed, err)
		}
		if got := queue.InflightCount(); got != 0 {
			t.Fatalf("expected inflight leases to return to 0, got %d", got)
		}
	})

	t.Run("executeQueuedTask empty item error releases lease", func(t *testing.T) {
		driver := storage.NewMemoryDriver()
		queue := &queuedLeaseSpy{inner: scheduler.NewMemoryQueue()}
		runner := New(Config{Storage: driver, WorkerID: "worker-a"})
		runner.queue = queue

		state := team.RunState{
			ID:         "team-execute",
			Pattern:    "linear",
			Status:     team.StatusRunning,
			Phase:      team.PhaseResearch,
			Supervisor: team.AgentInstance{ID: "supervisor", Role: team.RoleSupervisor, ProfileName: "supervisor"},
			Workers:    []team.AgentInstance{{ID: "worker-1", Role: team.RoleResearcher, ProfileName: "worker"}},
			Tasks: []team.Task{{
				ID:              "task-execute",
				Kind:            team.TaskKindResearch,
				Input:           "missing assignee",
				RequiredRole:    team.RoleResearcher,
				AssigneeAgentID: "worker-missing",
				FailurePolicy:   team.FailurePolicyFailFast,
				Status:          team.TaskStatusPending,
			}},
		}
		state.Normalize()
		if err := driver.Teams().Save(ctx, state); err != nil {
			t.Fatalf("Save() error = %v", err)
		}

		lease := queuedLeaseAcquire(t, ctx, queue, runner.workerID, scheduler.TaskLease{TeamID: state.ID, TaskID: state.Tasks[0].ID})
		err := runner.runQueuedLease(ctx, lease)
		if !errors.Is(err, ErrInvalidTeamState) {
			t.Fatalf("expected ErrInvalidTeamState, got %v", err)
		}
		if got := queue.InflightCount(); got != 0 {
			t.Fatalf("expected inflight leases to return to 0, got %d", got)
		}
	})

	t.Run("persistQueuedTaskState error releases lease", func(t *testing.T) {
		errSaveFailed := errors.New("save failed")
		baseDriver := storage.NewMemoryDriver()
		queue := &queuedLeaseSpy{inner: scheduler.NewMemoryQueue()}
		runner := New(Config{
			Storage: &queuedLeaseDriver{
				inner: baseDriver,
				teams: &queuedLeaseTeamStore{inner: baseDriver.Teams(), saveCASErr: errSaveFailed},
			},
			WorkerID: "worker-a",
		})
		runner.queue = queue
		runner.RegisterProvider("team-fake", teamFakeProvider{})
		runner.RegisterPattern(queuedLeasePattern{})
		runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "team-fake", Model: "test"})
		runner.RegisterProfile(team.Profile{Name: "worker", Role: team.RoleResearcher, Provider: "team-fake", Model: "test"})

		state := team.RunState{
			ID:         "team-persist",
			Pattern:    queuedLeasePatternName,
			Status:     team.StatusRunning,
			Phase:      team.PhaseResearch,
			Supervisor: team.AgentInstance{ID: "supervisor", Role: team.RoleSupervisor, ProfileName: "supervisor"},
			Workers:    []team.AgentInstance{{ID: "worker-1", Role: team.RoleResearcher, ProfileName: "worker"}},
			Tasks: []team.Task{{
				ID:              "task-persist",
				Kind:            team.TaskKindResearch,
				Input:           "persist failure",
				RequiredRole:    team.RoleResearcher,
				AssigneeAgentID: "worker-1",
				FailurePolicy:   team.FailurePolicyFailFast,
				Status:          team.TaskStatusPending,
			}},
		}
		state.Normalize()
		if err := baseDriver.Teams().Save(ctx, state); err != nil {
			t.Fatalf("Save() error = %v", err)
		}

		lease := queuedLeaseAcquire(t, ctx, queue, runner.workerID, scheduler.TaskLease{TeamID: state.ID, TaskID: state.Tasks[0].ID})
		err := runner.runQueuedLease(ctx, lease)
		if !errors.Is(err, errSaveFailed) {
			t.Fatalf("expected persist error %v, got %v", errSaveFailed, err)
		}
		if got := queue.InflightCount(); got != 0 {
			t.Fatalf("expected inflight leases to return to 0, got %d", got)
		}
	})
}

func TestQueuedLeaseRejectsForeignTeamLease(t *testing.T) {
	newQueueRuntime := func(t *testing.T, workerID string, queue scheduler.TaskQueue) *Runtime {
		t.Helper()
		runner := New(Config{Storage: storage.NewMemoryDriver(), WorkerID: workerID})
		if err := runner.RegisterPlugin(plugin.Spec{Type: plugin.TypeScheduler, Name: "memory-queue", Component: queue}); err != nil {
			t.Fatalf("RegisterPlugin() error = %v", err)
		}
		runner.RegisterProvider("team-fake", teamFakeProvider{})
		runner.RegisterPattern(singleTaskPattern{})
		runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "team-fake", Model: "test"})
		runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "team-fake", Model: "test"})
		return runner
	}

	newSingleTaskState := func(t *testing.T, teamID, input string) team.RunState {
		t.Helper()
		state, err := singleTaskPattern{}.Start(context.Background(), team.StartRequest{
			TeamID:            teamID,
			SupervisorProfile: "supervisor",
			WorkerProfiles:    []string{"researcher"},
			Input:             map[string]any{"task": input},
		})
		if err != nil {
			t.Fatalf("singleTaskPattern.Start() error = %v", err)
		}
		state.Normalize()
		return state
	}

	drainLeases := func(t *testing.T, queue scheduler.TaskQueue) []scheduler.TaskLease {
		t.Helper()
		var leases []scheduler.TaskLease
		for {
			lease, ok, err := queue.Acquire(context.Background(), "inspector", localQueueLeaseTTL)
			if err != nil {
				t.Fatalf("Acquire() error = %v", err)
			}
			if !ok {
				return leases
			}
			leases = append(leases, lease)
		}
	}

	runLease := func(t *testing.T, runner *Runtime, current team.RunState) (taskOutcome, bool) {
		t.Helper()
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("processQueuedLease() panicked: %v", recovered)
			}
		}()
		return runner.processQueuedLease(context.Background(), current, mapTaskIndexes(current.Tasks), nil)
	}

	t.Run("foreign team same task id is skipped in favor of local lease", func(t *testing.T) {
		queue := scheduler.NewMemoryQueue()
		worker := newQueueRuntime(t, "worker-a", queue)
		current := newSingleTaskState(t, "team-a", "team-a")
		foreign := newSingleTaskState(t, "team-b", "team-b")

		if err := queue.Enqueue(context.Background(), scheduler.TaskLease{TaskID: foreign.Tasks[0].ID, TeamID: foreign.ID}); err != nil {
			t.Fatalf("Enqueue(foreign) error = %v", err)
		}
		if err := queue.Enqueue(context.Background(), scheduler.TaskLease{TaskID: current.Tasks[0].ID, TeamID: current.ID}); err != nil {
			t.Fatalf("Enqueue(current) error = %v", err)
		}

		outcome, handled := runLease(t, worker, current)
		if !handled {
			t.Fatal("expected local lease to be processed")
		}
		if outcome.task.ID != current.Tasks[0].ID {
			t.Fatalf("expected local task outcome, got %#v", outcome)
		}

		remaining := drainLeases(t, queue)
		if len(remaining) != 1 {
			t.Fatalf("expected only foreign lease to remain available, got %#v", remaining)
		}
		if remaining[0].TeamID != foreign.ID || remaining[0].TaskID != foreign.Tasks[0].ID {
			t.Fatalf("expected foreign lease to remain queued, got %#v", remaining)
		}
	})

	t.Run("missing index with empty team id stays untouched while local lease proceeds", func(t *testing.T) {
		queue := scheduler.NewMemoryQueue()
		worker := newQueueRuntime(t, "worker-a", queue)
		current := newSingleTaskState(t, "team-a", "team-a")

		if err := queue.Enqueue(context.Background(), scheduler.TaskLease{TaskID: "ghost"}); err != nil {
			t.Fatalf("Enqueue(ghost) error = %v", err)
		}
		if err := queue.Enqueue(context.Background(), scheduler.TaskLease{TaskID: current.Tasks[0].ID, TeamID: current.ID}); err != nil {
			t.Fatalf("Enqueue(current) error = %v", err)
		}

		outcome, handled := runLease(t, worker, current)
		if !handled {
			t.Fatal("expected local lease to be processed")
		}
		if outcome.task.ID != current.Tasks[0].ID {
			t.Fatalf("expected local task outcome, got %#v", outcome)
		}

		remaining := drainLeases(t, queue)
		if len(remaining) != 1 {
			t.Fatalf("expected ghost lease to remain queued, got %#v", remaining)
		}
		if remaining[0].TaskID != "ghost" || remaining[0].TeamID != "" {
			t.Fatalf("expected ghost lease to remain queued, got %#v", remaining)
		}
	})
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

func assertQueuedConflictReason(t *testing.T, events []storage.Event, taskID string) {
	t.Helper()

	staleWriteReasons := make([]string, 0, 1)
	for _, event := range events {
		if event.TaskID != taskID {
			continue
		}
		reason, _ := event.Payload["reason"].(string)
		if reason == eventReasonHeartbeatExpired {
			t.Fatalf("unexpected lease-expiry reason for stale state conflict: %#v", event)
		}
		if event.Type == storage.EventStaleWriteRejected {
			staleWriteReasons = append(staleWriteReasons, reason)
		}
	}
	if len(staleWriteReasons) != 1 {
		t.Fatalf("expected exactly one stale write rejection event for %s, got %d in %#v", taskID, len(staleWriteReasons), events)
	}
	if staleWriteReasons[0] != eventReasonStateVersionConflict {
		t.Fatalf("expected stale write rejection reason %q, got %q", eventReasonStateVersionConflict, staleWriteReasons[0])
	}
}

type queuedLeaseSpy struct {
	inner *scheduler.MemoryQueue
}

func (q *queuedLeaseSpy) Enqueue(ctx context.Context, lease scheduler.TaskLease) error {
	return q.inner.Enqueue(ctx, lease)
}

func (q *queuedLeaseSpy) Acquire(ctx context.Context, ownerID string, ttl time.Duration) (scheduler.TaskLease, bool, error) {
	return q.inner.Acquire(ctx, ownerID, ttl)
}

func (q *queuedLeaseSpy) AcquireForTeam(ctx context.Context, ownerID, teamID string, ttl time.Duration) (scheduler.TaskLease, bool, error) {
	return q.inner.AcquireForTeam(ctx, ownerID, teamID, ttl)
}

func (q *queuedLeaseSpy) Heartbeat(ctx context.Context, lease scheduler.TaskLease, ttl time.Duration) error {
	return q.inner.Heartbeat(ctx, lease, ttl)
}

func (q *queuedLeaseSpy) Release(ctx context.Context, lease scheduler.TaskLease) error {
	return q.inner.Release(ctx, lease)
}

func (q *queuedLeaseSpy) RecoverExpired(ctx context.Context, now time.Time) error {
	return q.inner.RecoverExpired(ctx, now)
}

func (q *queuedLeaseSpy) InflightCount() int {
	return q.inner.InflightCount()
}

type queuedLeaseDriver struct {
	inner storage.Driver
	teams storage.TeamStore
}

func (d *queuedLeaseDriver) Sessions() session.Store {
	return d.inner.Sessions()
}

func (d *queuedLeaseDriver) Runs() storage.RunStore {
	return d.inner.Runs()
}

func (d *queuedLeaseDriver) Workflows() storage.WorkflowStore {
	return d.inner.Workflows()
}

func (d *queuedLeaseDriver) Teams() storage.TeamStore {
	return d.teams
}

func (d *queuedLeaseDriver) Artifacts() storage.ArtifactStore {
	return d.inner.Artifacts()
}

func (d *queuedLeaseDriver) Events() storage.EventStore {
	return d.inner.Events()
}

type queuedLeaseTeamStore struct {
	inner      storage.TeamStore
	loadErr    error
	saveCASErr error
}

func (s *queuedLeaseTeamStore) Save(ctx context.Context, state team.RunState) error {
	return s.inner.Save(ctx, state)
}

func (s *queuedLeaseTeamStore) SaveCAS(ctx context.Context, state team.RunState, expectedVersion int) (int, error) {
	if s.saveCASErr != nil {
		return 0, s.saveCASErr
	}
	return s.inner.SaveCAS(ctx, state, expectedVersion)
}

func (s *queuedLeaseTeamStore) Load(ctx context.Context, teamID string) (team.RunState, error) {
	if s.loadErr != nil {
		return team.RunState{}, s.loadErr
	}
	return s.inner.Load(ctx, teamID)
}

func (s *queuedLeaseTeamStore) List(ctx context.Context) ([]team.RunState, error) {
	return s.inner.List(ctx)
}

const queuedLeasePatternName = "queue-lease-test"

type queuedLeasePattern struct{}

func (queuedLeasePattern) Name() string {
	return queuedLeasePatternName
}

func (queuedLeasePattern) Start(_ context.Context, _ team.StartRequest) (team.RunState, error) {
	return team.RunState{}, nil
}

func (queuedLeasePattern) Advance(_ context.Context, state team.RunState) (team.RunState, error) {
	return state, nil
}

func queuedLeaseAcquire(t *testing.T, ctx context.Context, queue *queuedLeaseSpy, ownerID string, lease scheduler.TaskLease) scheduler.TaskLease {
	t.Helper()
	if err := queue.Enqueue(ctx, lease); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	acquired, ok, err := queue.Acquire(ctx, ownerID, localQueueLeaseTTL)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if !ok {
		t.Fatal("expected queued lease to be acquired")
	}
	if got := queue.InflightCount(); got != 1 {
		t.Fatalf("expected 1 inflight lease before run, got %d", got)
	}
	return acquired
}
