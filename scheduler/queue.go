package scheduler

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrQueueFull = errors.New("queue capacity exceeded")

type QueueConfig struct {
	MaxPending  int
	MaxInflight int
}

type TaskLease struct {
	TaskID    string    `json:"taskId"`
	TeamID    string    `json:"teamId,omitempty"`
	OwnerID   string    `json:"ownerId,omitempty"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

type TaskQueue interface {
	Enqueue(ctx context.Context, lease TaskLease) error
	Acquire(ctx context.Context, ownerID string, ttl time.Duration) (TaskLease, bool, error)
	Heartbeat(ctx context.Context, taskID, ownerID string, ttl time.Duration) error
	Release(ctx context.Context, taskID, ownerID string) error
	RecoverExpired(ctx context.Context, now time.Time) error
}

type MemoryQueue struct {
	mu       sync.Mutex
	pending  []TaskLease
	inflight map[string]TaskLease
	config   QueueConfig
}

func NewMemoryQueue() *MemoryQueue {
	return &MemoryQueue{
		inflight: map[string]TaskLease{},
	}
}

func NewMemoryQueueWithConfig(config QueueConfig) *MemoryQueue {
	return &MemoryQueue{
		inflight: map[string]TaskLease{},
		config:   config,
	}
}

func (q *MemoryQueue) Enqueue(_ context.Context, lease TaskLease) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, current := range q.pending {
		if current.TaskID == lease.TaskID {
			return nil
		}
	}
	if _, ok := q.inflight[lease.TaskID]; ok {
		return nil
	}
	if q.config.MaxPending > 0 && len(q.pending) >= q.config.MaxPending {
		return ErrQueueFull
	}
	q.pending = append(q.pending, lease)
	return nil
}

func (q *MemoryQueue) Acquire(_ context.Context, ownerID string, ttl time.Duration) (TaskLease, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.config.MaxInflight > 0 && len(q.inflight) >= q.config.MaxInflight {
		return TaskLease{}, false, nil
	}
	now := time.Now()
	for idx, lease := range q.pending {
		if current, ok := q.inflight[lease.TaskID]; ok && current.ExpiresAt.After(now) {
			continue
		}
		lease.OwnerID = ownerID
		lease.ExpiresAt = now.Add(ttl)
		q.inflight[lease.TaskID] = lease
		q.pending = append(q.pending[:idx], q.pending[idx+1:]...)
		return lease, true, nil
	}
	return TaskLease{}, false, nil
}

func (q *MemoryQueue) PendingCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}

func (q *MemoryQueue) InflightCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.inflight)
}

func (q *MemoryQueue) Heartbeat(_ context.Context, taskID, ownerID string, ttl time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	lease := q.inflight[taskID]
	if lease.OwnerID != ownerID {
		return nil
	}
	lease.ExpiresAt = time.Now().Add(ttl)
	q.inflight[taskID] = lease
	return nil
}

func (q *MemoryQueue) Release(_ context.Context, taskID, ownerID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	lease, ok := q.inflight[taskID]
	if ok && lease.OwnerID == ownerID {
		delete(q.inflight, taskID)
	}
	return nil
}

func (q *MemoryQueue) RecoverExpired(_ context.Context, now time.Time) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for taskID, lease := range q.inflight {
		if lease.ExpiresAt.Before(now) {
			lease.OwnerID = ""
			lease.ExpiresAt = time.Time{}
			q.pending = append(q.pending, lease)
			delete(q.inflight, taskID)
		}
	}
	return nil
}
