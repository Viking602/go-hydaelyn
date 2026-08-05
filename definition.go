package venat

import (
	"context"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/adapter"
)

// SaveAgentDefinitionSnapshot persists one immutable definition revision.
func (r *Runner) SaveAgentDefinitionSnapshot(ctx context.Context, snapshot api.AgentDefinitionSnapshot) error {
	converted, err := adapter.AgentDefinitionSnapshotToModel(snapshot)
	if err != nil {
		return err
	}
	return adapter.ErrorToAPI(r.rt.SaveAgentDefinitionSnapshot(ctx, converted))
}

// LoadAgentDefinitionSnapshot loads one immutable definition revision.
func (r *Runner) LoadAgentDefinitionSnapshot(ctx context.Context, definitionID, version string) (api.AgentDefinitionSnapshot, error) {
	snapshot, err := r.rt.LoadAgentDefinitionSnapshot(ctx, definitionID, version)
	if err != nil {
		return api.AgentDefinitionSnapshot{}, adapter.ErrorToAPI(err)
	}
	return adapter.AgentDefinitionSnapshotFromModel(snapshot)
}

// ListAgentDefinitionSnapshots lists immutable definition revisions matching selector.
func (r *Runner) ListAgentDefinitionSnapshots(ctx context.Context, selector api.AgentDefinitionSnapshotSelector) ([]api.AgentDefinitionSnapshot, error) {
	snapshots, err := r.rt.ListAgentDefinitionSnapshots(ctx, adapter.AgentDefinitionSnapshotSelectorToModel(selector))
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	out := make([]api.AgentDefinitionSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		converted, convertErr := adapter.AgentDefinitionSnapshotFromModel(snapshot)
		if convertErr != nil {
			return nil, convertErr
		}
		out = append(out, converted)
	}
	return out, nil
}
