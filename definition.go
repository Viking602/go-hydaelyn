package venat

import (
	"context"

	"github.com/Viking602/venat/api"
)

// SaveAgentDefinitionSnapshot persists one immutable definition revision.
func (r *Runner) SaveAgentDefinitionSnapshot(ctx context.Context, snapshot api.AgentDefinitionSnapshot) error {
	return r.rt.SaveAgentDefinitionSnapshot(ctx, snapshot)
}

// LoadAgentDefinitionSnapshot loads one immutable definition revision.
func (r *Runner) LoadAgentDefinitionSnapshot(ctx context.Context, definitionID, version string) (api.AgentDefinitionSnapshot, error) {
	return r.rt.LoadAgentDefinitionSnapshot(ctx, definitionID, version)
}

// ListAgentDefinitionSnapshots lists immutable definition revisions matching selector.
func (r *Runner) ListAgentDefinitionSnapshots(ctx context.Context, selector api.AgentDefinitionSnapshotSelector) ([]api.AgentDefinitionSnapshot, error) {
	return r.rt.ListAgentDefinitionSnapshots(ctx, selector)
}
