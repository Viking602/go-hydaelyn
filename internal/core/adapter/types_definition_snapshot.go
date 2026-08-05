package adapter

import (
	"encoding/json"
	"fmt"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/model"
)

func AgentDefinitionSnapshotToModel(in api.AgentDefinitionSnapshot) (model.AgentDefinitionSnapshot, error) {
	definition, err := json.Marshal(in.Definition)
	if err != nil {
		return model.AgentDefinitionSnapshot{}, fmt.Errorf("encode agent definition snapshot: %w", err)
	}
	return model.AgentDefinitionSnapshot{
		DefinitionID: in.Definition.ID,
		Version:      in.Definition.Version,
		Definition:   definition,
		Digest:       in.Digest,
		CreatedAt:    in.CreatedAt,
	}, nil
}

func AgentDefinitionSnapshotFromModel(in model.AgentDefinitionSnapshot) (api.AgentDefinitionSnapshot, error) {
	var definition api.AgentDefinition
	if err := json.Unmarshal(in.Definition, &definition); err != nil {
		return api.AgentDefinitionSnapshot{}, fmt.Errorf("decode agent definition snapshot: %w", err)
	}
	if definition.ID != in.DefinitionID || definition.Version != in.Version {
		return api.AgentDefinitionSnapshot{}, fmt.Errorf("agent definition snapshot identity mismatch: %w", model.ErrInvalidConfiguration)
	}
	return api.AgentDefinitionSnapshot{
		Definition: definition,
		Digest:     in.Digest,
		CreatedAt:  in.CreatedAt,
	}, nil
}

func AgentDefinitionSnapshotSelectorToModel(in api.AgentDefinitionSnapshotSelector) model.AgentDefinitionSnapshotSelector {
	return model.AgentDefinitionSnapshotSelector{
		DefinitionIDs: cloneStrings(in.DefinitionIDs),
		Versions:      cloneStrings(in.Versions),
		Since:         in.Since,
		Limit:         in.Limit,
	}
}

func AgentDefinitionSnapshotSelectorFromModel(in model.AgentDefinitionSnapshotSelector) api.AgentDefinitionSnapshotSelector {
	return api.AgentDefinitionSnapshotSelector{
		DefinitionIDs: cloneStrings(in.DefinitionIDs),
		Versions:      cloneStrings(in.Versions),
		Since:         in.Since,
		Limit:         in.Limit,
	}
}
