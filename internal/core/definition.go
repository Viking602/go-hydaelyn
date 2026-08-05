package core

import (
	"context"
	"fmt"

	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
)

// SaveAgentDefinitionSnapshot persists one immutable definition revision.
func (r *Runtime) SaveAgentDefinitionSnapshot(ctx context.Context, snapshot model.AgentDefinitionSnapshot) error {
	uow, err := r.beginWriteUoW(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = uow.Rollback(ctx)
		}
	}()
	store, err := r.agentDefinitionStore(ctx, uow)
	if err != nil {
		return err
	}
	if err := store.SaveAgentDefinitionSnapshot(ctx, snapshot); err != nil {
		return err
	}
	if err := uow.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

// LoadAgentDefinitionSnapshot loads one immutable definition revision.
func (r *Runtime) LoadAgentDefinitionSnapshot(ctx context.Context, definitionID, version string) (model.AgentDefinitionSnapshot, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return model.AgentDefinitionSnapshot{}, err
	}
	defer done()
	store, err := r.agentDefinitionStore(ctx, uow)
	if err != nil {
		return model.AgentDefinitionSnapshot{}, err
	}
	return store.LoadAgentDefinitionSnapshot(ctx, definitionID, version)
}

// ListAgentDefinitionSnapshots lists immutable definition revisions matching selector.
func (r *Runtime) ListAgentDefinitionSnapshots(ctx context.Context, selector model.AgentDefinitionSnapshotSelector) ([]model.AgentDefinitionSnapshot, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	store, err := r.agentDefinitionStore(ctx, uow)
	if err != nil {
		return nil, err
	}
	return store.ListAgentDefinitionSnapshots(ctx, selector)
}

func (r *Runtime) agentDefinitionStore(ctx context.Context, uow ports.UnitOfWork) (ports.AgentDefinitionStore, error) {
	capabilities, err := r.StoreCapabilities(ctx)
	if err != nil {
		return nil, err
	}
	if !capabilities.SupportsDefinitionSnapshots {
		return nil, fmt.Errorf("agent definition snapshot storage is not supported: %w", model.ErrInvalidConfiguration)
	}
	extension, ok := uow.(ports.AgentDefinitionUnitOfWork)
	if !ok {
		return nil, fmt.Errorf("provider advertises agent definition snapshots without exposing the store: %w", model.ErrInvalidConfiguration)
	}
	store := extension.AgentDefinitions()
	if store == nil {
		return nil, fmt.Errorf("provider advertises agent definition snapshots with a nil store: %w", model.ErrInvalidConfiguration)
	}
	return store, nil
}
