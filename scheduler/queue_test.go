package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestMemoryQueueEnqueueAcquireAndRelease(t *testing.T) {
	queue := NewMemoryQueue()
	if err := queue.Enqueue(context.Background(), TaskLease{
		TaskID: "task-1",
		TeamID: "team-1",
	}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	lease, ok, err := queue.Acquire(context.Background(), "worker-1", time.Minute)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if !ok || lease.TaskID != "task-1" || lease.OwnerID != "worker-1" {
		t.Fatalf("unexpected lease %#v", lease)
	}
	if err := queue.Release(context.Background(), lease.TaskID, lease.OwnerID); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
}

func TestMemoryQueueBackpressureRejectsPendingOverflow(t *testing.T) {
	queue := NewMemoryQueueWithConfig(QueueConfig{MaxPending: 2})
	for i := 0; i < 2; i++ {
		if err := queue.Enqueue(context.Background(), TaskLease{TaskID: fmt.Sprintf("task-%d", i)}); err != nil {
			t.Fatalf("Enqueue(%d) error = %v", i, err)
		}
	}
	err := queue.Enqueue(context.Background(), TaskLease{TaskID: "task-overflow"})
	if err != ErrQueueFull {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}
	if queue.PendingCount() != 2 {
		t.Fatalf("expected 2 pending, got %d", queue.PendingCount())
	}
}

func TestMemoryQueueBackpressureLimitsInflight(t *testing.T) {
	queue := NewMemoryQueueWithConfig(QueueConfig{MaxInflight: 1})
	_ = queue.Enqueue(context.Background(), TaskLease{TaskID: "task-a"})
	_ = queue.Enqueue(context.Background(), TaskLease{TaskID: "task-b"})
	_, ok, err := queue.Acquire(context.Background(), "w1", time.Minute)
	if err != nil || !ok {
		t.Fatalf("first Acquire should succeed, ok=%v err=%v", ok, err)
	}
	_, ok, err = queue.Acquire(context.Background(), "w2", time.Minute)
	if err != nil {
		t.Fatalf("second Acquire error = %v", err)
	}
	if ok {
		t.Fatalf("second Acquire should be blocked by MaxInflight")
	}
	if queue.InflightCount() != 1 {
		t.Fatalf("expected 1 inflight, got %d", queue.InflightCount())
	}
}

func TestMemoryQueueHeartbeatExtendsLeaseAndRecoverExpiredLease(t *testing.T) {
	queue := NewMemoryQueue()
	if err := queue.Enqueue(context.Background(), TaskLease{
		TaskID: "task-2",
		TeamID: "team-1",
	}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	lease, ok, err := queue.Acquire(context.Background(), "worker-1", 20*time.Millisecond)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if !ok {
		t.Fatalf("expected acquired lease")
	}
	if err := queue.Heartbeat(context.Background(), lease.TaskID, lease.OwnerID, time.Minute); err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	time.Sleep(25 * time.Millisecond)
	if _, ok, err := queue.Acquire(context.Background(), "worker-2", time.Minute); err != nil || ok {
		t.Fatalf("lease should still be held after heartbeat, got ok=%v err=%v", ok, err)
	}
	if err := queue.RecoverExpired(context.Background(), time.Now().Add(2*time.Minute)); err != nil {
		t.Fatalf("RecoverExpired() error = %v", err)
	}
	recovered, ok, err := queue.Acquire(context.Background(), "worker-2", time.Minute)
	if err != nil {
		t.Fatalf("Acquire() after recover error = %v", err)
	}
	if !ok || recovered.OwnerID != "worker-2" {
		t.Fatalf("expected recovered lease for worker-2, got %#v", recovered)
	}
}
