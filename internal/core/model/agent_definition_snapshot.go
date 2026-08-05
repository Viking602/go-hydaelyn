package model

import (
	"encoding/json"
	"time"
)

// AgentDefinitionSnapshot is the storage representation of one immutable
// AgentDefinition version. Definition contains the complete public definition
// encoded as JSON so the core model remains independent of the public API.
type AgentDefinitionSnapshot struct {
	DefinitionID string
	Version      string
	Definition   json.RawMessage
	Digest       string
	CreatedAt    time.Time
}

// AgentDefinitionSnapshotSelector filters stored definition snapshots.
type AgentDefinitionSnapshotSelector struct {
	DefinitionIDs []string
	Versions      []string
	Since         time.Time
	Limit         int
}
