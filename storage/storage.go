package storage

import (
	"context"
	"sync"
	"time"

	"hydaelyn/session"
	"hydaelyn/team"
	"hydaelyn/workflow"
)

type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
	RunStatusAborted   RunStatus = "aborted"
)

type Run struct {
	ID        string            `json:"id"`
	SessionID string            `json:"sessionId,omitempty"`
	Status    RunStatus         `json:"status"`
	Provider  string            `json:"provider,omitempty"`
	Model     string            `json:"model,omitempty"`
	Error     string            `json:"error,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

type RunStore interface {
	Save(ctx context.Context, run Run) error
	Load(ctx context.Context, runID string) (Run, error)
	List(ctx context.Context) ([]Run, error)
}

type WorkflowStore interface {
	Save(ctx context.Context, state workflow.State) error
	Load(ctx context.Context, workflowID string) (workflow.State, error)
	List(ctx context.Context) ([]workflow.State, error)
}

type TeamStore interface {
	Save(ctx context.Context, state team.RunState) error
	Load(ctx context.Context, teamID string) (team.RunState, error)
	List(ctx context.Context) ([]team.RunState, error)
}

type Artifact struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	MIMEType  string            `json:"mimeType,omitempty"`
	Data      []byte            `json:"data,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"createdAt"`
}

type ArtifactStore interface {
	Save(ctx context.Context, artifact Artifact) error
	Load(ctx context.Context, artifactID string) (Artifact, error)
	List(ctx context.Context) ([]Artifact, error)
}

type Driver interface {
	Sessions() session.Store
	Runs() RunStore
	Workflows() WorkflowStore
	Teams() TeamStore
	Artifacts() ArtifactStore
}

type MemoryDriver struct {
	sessions  session.Store
	runs      *memoryRunStore
	workflows *memoryWorkflowStore
	teams     *memoryTeamStore
	artifacts *memoryArtifactStore
}

func NewMemoryDriver() *MemoryDriver {
	return &MemoryDriver{
		sessions:  session.NewMemoryStore(),
		runs:      &memoryRunStore{runs: map[string]Run{}},
		workflows: &memoryWorkflowStore{items: map[string]workflow.State{}},
		teams:     &memoryTeamStore{items: map[string]team.RunState{}},
		artifacts: &memoryArtifactStore{items: map[string]Artifact{}},
	}
}

func (d *MemoryDriver) Sessions() session.Store {
	return d.sessions
}

func (d *MemoryDriver) Runs() RunStore {
	return d.runs
}

func (d *MemoryDriver) Workflows() WorkflowStore {
	return d.workflows
}

func (d *MemoryDriver) Teams() TeamStore {
	return d.teams
}

func (d *MemoryDriver) Artifacts() ArtifactStore {
	return d.artifacts
}

type memoryRunStore struct {
	mu   sync.RWMutex
	runs map[string]Run
}

func (s *memoryRunStore) Save(_ context.Context, run Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}
	run.UpdatedAt = time.Now().UTC()
	s.runs[run.ID] = run
	return nil
}

func (s *memoryRunStore) Load(_ context.Context, runID string) (Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.runs[runID], nil
}

func (s *memoryRunStore) List(_ context.Context) ([]Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]Run, 0, len(s.runs))
	for _, run := range s.runs {
		items = append(items, run)
	}
	return items, nil
}

type memoryWorkflowStore struct {
	mu    sync.RWMutex
	items map[string]workflow.State
}

func (s *memoryWorkflowStore) Save(_ context.Context, state workflow.State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state.UpdatedAt = time.Now().UTC()
	if state.CreatedAt.IsZero() {
		state.CreatedAt = state.UpdatedAt
	}
	s.items[state.ID] = state
	return nil
}

func (s *memoryWorkflowStore) Load(_ context.Context, workflowID string) (workflow.State, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.items[workflowID], nil
}

func (s *memoryWorkflowStore) List(_ context.Context) ([]workflow.State, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]workflow.State, 0, len(s.items))
	for _, state := range s.items {
		items = append(items, state)
	}
	return items, nil
}

type memoryArtifactStore struct {
	mu    sync.RWMutex
	items map[string]Artifact
}

type memoryTeamStore struct {
	mu    sync.RWMutex
	items map[string]team.RunState
}

func (s *memoryTeamStore) Save(_ context.Context, state team.RunState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state.UpdatedAt = time.Now().UTC()
	if state.CreatedAt.IsZero() {
		state.CreatedAt = state.UpdatedAt
	}
	s.items[state.ID] = state
	return nil
}

func (s *memoryTeamStore) Load(_ context.Context, teamID string) (team.RunState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.items[teamID], nil
}

func (s *memoryTeamStore) List(_ context.Context) ([]team.RunState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]team.RunState, 0, len(s.items))
	for _, state := range s.items {
		items = append(items, state)
	}
	return items, nil
}

func (s *memoryArtifactStore) Save(_ context.Context, artifact Artifact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if artifact.CreatedAt.IsZero() {
		artifact.CreatedAt = time.Now().UTC()
	}
	s.items[artifact.ID] = artifact
	return nil
}

func (s *memoryArtifactStore) Load(_ context.Context, artifactID string) (Artifact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.items[artifactID], nil
}

func (s *memoryArtifactStore) List(_ context.Context) ([]Artifact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]Artifact, 0, len(s.items))
	for _, artifact := range s.items {
		items = append(items, artifact)
	}
	return items, nil
}
