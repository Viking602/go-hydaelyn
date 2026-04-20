package workflow

import (
	"context"
	"sync"
	"time"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusPaused    Status = "paused"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusAborted   Status = "aborted"
)

type State struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Status    Status            `json:"status"`
	Step      string            `json:"step,omitempty"`
	Phase     string            `json:"phase,omitempty"`
	Tasks     []TaskState       `json:"tasks,omitempty"`
	ChildRuns []ChildRunState   `json:"childRuns,omitempty"`
	Retry     RetryPolicy       `json:"retry,omitempty"`
	Abort     *AbortState       `json:"abort,omitempty"`
	Data      map[string]any    `json:"data,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

type TaskState struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind,omitempty"`
	Assignee  string   `json:"assignee,omitempty"`
	Status    string   `json:"status,omitempty"`
	DependsOn []string `json:"dependsOn,omitempty"`
	SessionID string   `json:"sessionId,omitempty"`
	Error     string   `json:"error,omitempty"`
}

type ChildRunState struct {
	TaskID    string `json:"taskId"`
	AgentID   string `json:"agentId,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	Status    string `json:"status,omitempty"`
	Error     string `json:"error,omitempty"`
}

type RetryPolicy struct {
	MaxAttempts int `json:"maxAttempts,omitempty"`
	Attempts    int `json:"attempts,omitempty"`
}

type AbortState struct {
	Reason      string    `json:"reason,omitempty"`
	RequestedAt time.Time `json:"requestedAt,omitempty"`
}

type Driver interface {
	Name() string
	Start(ctx context.Context, input map[string]any) (State, error)
	Resume(ctx context.Context, state State) (State, error)
	Abort(ctx context.Context, state State) (State, error)
}

type Registry struct {
	mu      sync.RWMutex
	drivers map[string]Driver
}

func NewRegistry(drivers ...Driver) *Registry {
	r := &Registry{
		drivers: make(map[string]Driver, len(drivers)),
	}
	for _, driver := range drivers {
		r.Register(driver)
	}
	return r
}

func (r *Registry) Register(driver Driver) {
	if driver == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.drivers[driver.Name()] = driver
}

func (r *Registry) Driver(name string) (Driver, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	driver, ok := r.drivers[name]
	return driver, ok
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.drivers))
	for name := range r.drivers {
		names = append(names, name)
	}
	return names
}
