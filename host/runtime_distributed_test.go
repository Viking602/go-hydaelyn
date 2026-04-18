package host

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/patterns/deepsearch"
	"github.com/Viking602/go-hydaelyn/plugin"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/scheduler"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

func newDistributedRuntime(workerID string, driver storage.Driver, queue scheduler.TaskQueue) *Runtime {
	runtime := New(Config{
		Storage:  driver,
		WorkerID: workerID,
	})
	_ = runtime.RegisterPlugin(plugin.Spec{
		Type:      plugin.TypeScheduler,
		Name:      "memory-queue",
		Component: queue,
	})
	runtime.RegisterProvider("team-fake", teamFakeProvider{})
	runtime.RegisterPattern(deepsearch.New())
	runtime.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "team-fake", Model: "test"})
	runtime.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "team-fake", Model: "test"})
	return runtime
}

func TestDistributedRuntimesCanShareQueueAndStorage(t *testing.T) {
	driver := storage.NewMemoryDriver()
	queue := scheduler.NewMemoryQueue()
	coordinator := newDistributedRuntime("coordinator", driver, queue)
	worker := newDistributedRuntime("worker-b", driver, queue)

	state, err := coordinator.QueueTeam(context.Background(), StartTeamRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher", "researcher"},
		Input: map[string]any{
			"query":      "distributed",
			"subqueries": []string{"branch-a", "branch-b"},
		},
	})
	if err != nil {
		t.Fatalf("QueueTeam() error = %v", err)
	}
	if state.Status != team.StatusRunning {
		t.Fatalf("expected queued team to be running/pending execution, got %#v", state)
	}
	if len(state.Tasks) != 2 || state.Tasks[0].Status != team.TaskStatusPending {
		t.Fatalf("expected queued pending tasks, got %#v", state.Tasks)
	}
	processed, err := worker.RunQueueWorker(context.Background(), 10)
	if err != nil {
		t.Fatalf("RunQueueWorker() error = %v", err)
	}
	if processed == 0 {
		t.Fatalf("expected worker to process queued tasks")
	}
	current, err := coordinator.GetTeam(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("GetTeam() error = %v", err)
	}
	if current.Status != team.StatusCompleted {
		t.Fatalf("expected worker to complete shared team, got %#v", current)
	}
}

type heartbeatDroppingQueue struct {
	inner scheduler.TaskQueue
}

func (q *heartbeatDroppingQueue) Enqueue(ctx context.Context, lease scheduler.TaskLease) error {
	return q.inner.Enqueue(ctx, lease)
}

func (q *heartbeatDroppingQueue) Acquire(ctx context.Context, ownerID string, ttl time.Duration) (scheduler.TaskLease, bool, error) {
	return q.inner.Acquire(ctx, ownerID, ttl)
}

func (q *heartbeatDroppingQueue) Heartbeat(_ context.Context, _ scheduler.TaskLease, _ time.Duration) error {
	return nil
}

func (q *heartbeatDroppingQueue) Release(ctx context.Context, lease scheduler.TaskLease) error {
	return q.inner.Release(ctx, lease)
}

func (q *heartbeatDroppingQueue) RecoverExpired(ctx context.Context, now time.Time) error {
	return q.inner.RecoverExpired(ctx, now)
}

type leaseRaceProvider struct {
	mu           sync.Mutex
	calls        int
	firstStarted chan struct{}
	secondStart  chan struct{}
	release      chan struct{}
}

func newLeaseRaceProvider() *leaseRaceProvider {
	return &leaseRaceProvider{
		firstStarted: make(chan struct{}),
		secondStart:  make(chan struct{}),
		release:      make(chan struct{}),
	}
}

func (p *leaseRaceProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "lease-race"}
}

func (p *leaseRaceProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	last := request.Messages[len(request.Messages)-1].Text
	p.mu.Lock()
	p.calls++
	calls := p.calls
	p.mu.Unlock()
	if strings.Contains(last, "lease-race") {
		switch calls {
		case 1:
			close(p.firstStarted)
		case 2:
			close(p.secondStart)
		}
		<-p.release
	}
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: last},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

func (p *leaseRaceProvider) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

type singleTaskPattern struct{}

func (singleTaskPattern) Name() string { return "single-task" }

func (singleTaskPattern) Start(_ context.Context, request team.StartRequest) (team.RunState, error) {
	input, _ := request.Input["task"].(string)
	return team.RunState{
		ID:      request.TeamID,
		Pattern: "single-task",
		Status:  team.StatusRunning,
		Phase:   team.PhaseResearch,
		Supervisor: team.AgentInstance{ID: "supervisor", Role: team.RoleSupervisor, ProfileName: request.SupervisorProfile},
		Workers: []team.AgentInstance{{ID: "worker-1", Role: team.RoleResearcher, ProfileName: request.WorkerProfiles[0]}},
		Tasks: []team.Task{{
			ID:              "task-1",
			Kind:            team.TaskKindResearch,
			Input:           input,
			RequiredRole:    team.RoleResearcher,
			AssigneeAgentID: "worker-1",
			FailurePolicy:   team.FailurePolicyFailFast,
			Namespace:       "impl.task-1",
			Writes:          []string{"result"},
			Publish:         []team.OutputVisibility{team.OutputVisibilityBlackboard},
			Status:          team.TaskStatusPending,
		}},
		Input: request.Input,
	}, nil
}

func (singleTaskPattern) Advance(_ context.Context, state team.RunState) (team.RunState, error) {
	for _, task := range state.Tasks {
		if task.Status == team.TaskStatusPending || task.Status == team.TaskStatusRunning {
			return state, nil
		}
	}
	state.Status = team.StatusCompleted
	state.Phase = team.PhaseComplete
	state.Result = &team.Result{Summary: "done"}
	return state, nil
}

func TestMultiAgentCollaboration_LeaseExpiryDoesNotDuplicateCommit(t *testing.T) {
	driver := storage.NewMemoryDriver()
	queue := &heartbeatDroppingQueue{inner: scheduler.NewMemoryQueue()}
	provider := newLeaseRaceProvider()
	newRuntime := func(workerID string) *Runtime {
		runtime := New(Config{Storage: driver, WorkerID: workerID})
		if err := runtime.RegisterPlugin(plugin.Spec{Type: plugin.TypeScheduler, Name: "memory-queue", Component: queue}); err != nil {
			t.Fatalf("RegisterPlugin() error = %v", err)
		}
		runtime.RegisterProvider("lease-race", provider)
		runtime.RegisterPattern(singleTaskPattern{})
		runtime.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "lease-race", Model: "test"})
		runtime.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "lease-race", Model: "test"})
		return runtime
	}
	coordinator := newRuntime("coordinator")
	workerA := newRuntime("worker-a")
	workerB := newRuntime("worker-b")

	state, err := coordinator.QueueTeam(context.Background(), StartTeamRequest{
		Pattern:           "single-task",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input:             map[string]any{"task": "lease-race"},
	})
	if err != nil {
		t.Fatalf("QueueTeam() error = %v", err)
	}
	workerErrs := make(chan error, 2)
	go func() {
		_, err := workerA.RunQueueWorker(context.Background(), 1)
		workerErrs <- err
	}()
	select {
	case <-provider.firstStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first queued execution")
	}
	time.Sleep(localQueueLeaseTTL + 15*time.Millisecond)
	if err := coordinator.RecoverQueueLeases(context.Background(), time.Now().Add(localQueueLeaseTTL)); err != nil {
		t.Fatalf("RecoverQueueLeases() error = %v", err)
	}
	go func() {
		_, err := workerB.RunQueueWorker(context.Background(), 1)
		workerErrs <- err
	}()
	select {
	case <-provider.secondStart:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for duplicate queued execution")
	}
	close(provider.release)
	for range 2 {
		if err := <-workerErrs; err != nil {
			t.Fatalf("RunQueueWorker() error = %v", err)
		}
	}
	current, err := coordinator.GetTeam(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("GetTeam() error = %v", err)
	}
	if current.Status != team.StatusCompleted {
		t.Fatalf("expected completed state, got %#v", current)
	}
	if provider.Calls() != 2 {
		t.Fatalf("expected duplicated execution attempt, got %d calls", provider.Calls())
	}
	task := current.Tasks[0]
	if task.CompletedAt.IsZero() {
		t.Fatalf("expected committed completion timestamp, got %#v", task)
	}
	if task.CompletedBy != "worker-a" && task.CompletedBy != "worker-b" {
		t.Fatalf("expected committed worker id, got %#v", task)
	}
	if current.Blackboard == nil {
		t.Fatalf("expected blackboard state, got %#v", current)
	}
	assertSingleCommittedOutput(t, *current.Blackboard, task.ID, "result", "lease-race")
	if _, ok, err := queue.Acquire(context.Background(), "worker-c", time.Minute); err != nil || ok {
		t.Fatalf("expected queue to be empty after authoritative commit, got ok=%v err=%v", ok, err)
	}
}
