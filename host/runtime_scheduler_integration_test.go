package host

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/patterns/deepsearch"
	"github.com/Viking602/go-hydaelyn/plugin"
	"github.com/Viking602/go-hydaelyn/scheduler"
	"github.com/Viking602/go-hydaelyn/team"
)

type queueSpy struct {
	mu         sync.Mutex
	inner      scheduler.TaskQueue
	enqueues   int
	acquires   int
	heartbeats int
	releases   int
	owners     []string
}

func (q *queueSpy) Enqueue(ctx context.Context, lease scheduler.TaskLease) error {
	q.mu.Lock()
	q.enqueues++
	q.mu.Unlock()
	return q.inner.Enqueue(ctx, lease)
}

func (q *queueSpy) Acquire(ctx context.Context, ownerID string, ttl time.Duration) (scheduler.TaskLease, bool, error) {
	q.mu.Lock()
	q.acquires++
	q.owners = append(q.owners, ownerID)
	q.mu.Unlock()
	return q.inner.Acquire(ctx, ownerID, ttl)
}

func (q *queueSpy) Heartbeat(ctx context.Context, lease scheduler.TaskLease, ttl time.Duration) error {
	q.mu.Lock()
	q.heartbeats++
	q.owners = append(q.owners, lease.OwnerID)
	q.mu.Unlock()
	return q.inner.Heartbeat(ctx, lease, ttl)
}

func (q *queueSpy) Release(ctx context.Context, lease scheduler.TaskLease) error {
	q.mu.Lock()
	q.releases++
	q.owners = append(q.owners, lease.OwnerID)
	q.mu.Unlock()
	return q.inner.Release(ctx, lease)
}

func (q *queueSpy) RecoverExpired(ctx context.Context, now time.Time) error {
	return q.inner.RecoverExpired(ctx, now)
}

func TestRuntimeUsesSchedulerQueueWhenRegistered(t *testing.T) {
	queue := &queueSpy{inner: scheduler.NewMemoryQueue()}
	runtime := New(Config{})
	if err := runtime.RegisterPlugin(plugin.Spec{
		Type:      plugin.TypeScheduler,
		Name:      "memory-queue",
		Component: queue,
	}); err != nil {
		t.Fatalf("RegisterPlugin() error = %v", err)
	}
	runtime.RegisterProvider("team-fake", teamFakeProvider{})
	runtime.RegisterPattern(deepsearch.New())
	runtime.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "team-fake", Model: "test"})
	runtime.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "team-fake", Model: "test"})

	state, err := runtime.StartTeam(context.Background(), StartTeamRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher", "researcher"},
		Input: map[string]any{
			"query":      "queue integration",
			"subqueries": []string{"branch-a", "branch-b"},
		},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	if state.Status != team.StatusCompleted {
		t.Fatalf("expected completed state, got %#v", state)
	}
	queue.mu.Lock()
	enqueues, acquires, releases := queue.enqueues, queue.acquires, queue.releases
	queue.mu.Unlock()
	if enqueues == 0 || acquires == 0 || releases == 0 {
		t.Fatalf("expected queue lifecycle calls, got enq=%d acq=%d rel=%d", enqueues, acquires, releases)
	}
	if _, ok, err := queue.Acquire(context.Background(), "worker-3", time.Minute); err != nil || ok {
		t.Fatalf("expected queue to be empty after run, got ok=%v err=%v", ok, err)
	}
}

func TestRuntimeQueueWorkerHeartbeatsLongRunningTask(t *testing.T) {
	queue := &queueSpy{inner: scheduler.NewMemoryQueue()}
	runtime := New(Config{WorkerID: "worker-a"})
	if err := runtime.RegisterPlugin(plugin.Spec{
		Type:      plugin.TypeScheduler,
		Name:      "memory-queue",
		Component: queue,
	}); err != nil {
		t.Fatalf("RegisterPlugin() error = %v", err)
	}
	runtime.RegisterProvider("team-fake", teamFakeProvider{})
	runtime.RegisterPattern(deepsearch.New())
	runtime.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "team-fake", Model: "test"})
	runtime.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "team-fake", Model: "test"})

	state, err := runtime.StartTeam(context.Background(), StartTeamRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input: map[string]any{
			"query":      "queue heartbeat",
			"subqueries": []string{"slow research branch"},
		},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	if state.Status != team.StatusCompleted {
		t.Fatalf("expected completed state, got %#v", state)
	}
	queue.mu.Lock()
	heartbeats := queue.heartbeats
	owners := append([]string{}, queue.owners...)
	queue.mu.Unlock()
	if heartbeats == 0 {
		t.Fatalf("expected queue heartbeat calls for long-running task")
	}
	for _, owner := range owners {
		if owner != "worker-a" {
			t.Fatalf("expected runtime worker id to flow through queue calls, got owners %#v", owners)
		}
	}
}
