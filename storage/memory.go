package storage

import (
	"context"
	"sync"
	"time"

	"github.com/Viking602/go-hydaelyn/session"
	"github.com/Viking602/go-hydaelyn/team"
	"github.com/Viking602/go-hydaelyn/workflow"
)

type MemoryDriver struct {
	sessions  session.Store
	runs      *memoryRunStore
	workflows *memoryWorkflowStore
	teams     *memoryTeamStore
	artifacts *memoryArtifactStore
	events    *memoryEventStore
}

func NewMemoryDriver() *MemoryDriver {
	return &MemoryDriver{
		sessions:  session.NewMemoryStore(),
		runs:      &memoryRunStore{runs: map[string]Run{}},
		workflows: &memoryWorkflowStore{items: map[string]workflow.State{}},
		teams:     &memoryTeamStore{items: map[string]team.RunState{}},
		artifacts: &memoryArtifactStore{items: map[string]Artifact{}},
		events:    &memoryEventStore{items: map[string][]Event{}},
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

func (d *MemoryDriver) Events() EventStore {
	return d.events
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

type memoryEventStore struct {
	mu    sync.RWMutex
	items map[string][]Event
}

type memoryTeamStore struct {
	mu    sync.RWMutex
	items map[string]team.RunState
}

func (s *memoryTeamStore) Save(_ context.Context, state team.RunState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state = prepareStoredTeamState(state, s.items[state.ID], true)
	s.items[state.ID] = state
	return nil
}

func (s *memoryTeamStore) SaveCAS(_ context.Context, state team.RunState, expectedVersion int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	currentVersion := 0
	if existing, ok := s.items[state.ID]; ok {
		currentVersion = normalizedRunStateVersion(existing.Version)
	}
	if expectedVersion != currentVersion {
		return currentVersion, ErrStaleState
	}
	state = prepareStoredTeamState(state, s.items[state.ID], false)
	s.items[state.ID] = state
	return state.Version, nil
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

func prepareStoredTeamState(state team.RunState, existing team.RunState, autoIncrement bool) team.RunState {
	now := time.Now().UTC()
	state.UpdatedAt = now
	if !existing.CreatedAt.IsZero() {
		state.CreatedAt = existing.CreatedAt
	}
	if state.CreatedAt.IsZero() {
		state.CreatedAt = now
	}
	currentVersion := normalizedRunStateVersion(existing.Version)
	if existing.ID == "" {
		currentVersion = 0
	}
	if autoIncrement {
		if currentVersion == 0 {
			if state.Version <= 0 {
				state.Version = 1
			}
		} else if state.Version <= currentVersion {
			state.Version = currentVersion + 1
		}
		return state
	}
	state.Version = currentVersion + 1
	if state.Version <= 0 {
		state.Version = 1
	}
	return state
}

func normalizedRunStateVersion(version int) int {
	if version <= 0 {
		return 1
	}
	return version
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

func (s *memoryEventStore) Append(_ context.Context, event Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if event.Sequence == 0 {
		event.Sequence = len(s.items[event.RunID]) + 1
	}
	s.items[event.RunID] = append(s.items[event.RunID], event)
	return nil
}

func (s *memoryEventStore) List(_ context.Context, runID string) ([]Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Event{}, s.items[runID]...), nil
}
