package storage

import (
	"context"
	"errors"

	"github.com/Viking602/go-hydaelyn/session"
	"github.com/Viking602/go-hydaelyn/team"
	"github.com/Viking602/go-hydaelyn/workflow"
)

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
	SaveCAS(ctx context.Context, state team.RunState, expectedVersion int) (int, error)
	Load(ctx context.Context, teamID string) (team.RunState, error)
	List(ctx context.Context) ([]team.RunState, error)
}

var ErrStaleState = errors.New("stale team state")

type ArtifactStore interface {
	Save(ctx context.Context, artifact Artifact) error
	Load(ctx context.Context, artifactID string) (Artifact, error)
	List(ctx context.Context) ([]Artifact, error)
}

type EventStore interface {
	Append(ctx context.Context, event Event) error
	List(ctx context.Context, runID string) ([]Event, error)
}

type Driver interface {
	Sessions() session.Store
	Runs() RunStore
	Workflows() WorkflowStore
	Teams() TeamStore
	Artifacts() ArtifactStore
	Events() EventStore
}
