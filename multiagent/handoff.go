package multiagent

import (
	"encoding/json"
	"time"
)

// Handoff is the typed primitive multiagent schedulers use to move work
// from one AgentInstance to another. Distinct from api.HandoffRequest
// (the durable runner's persistence shape) — Handoff is the in-memory
// scheduler-side value; the runner durably records its persistence twin
// via the HandoffStore.
//
// RequiredOutputSchema is the schema the receiving AgentInstance is
// expected to honor — its OutputPolicy at construction time. This is
// load-bearing for the typed-handoff contract: a receiving agent that
// cannot satisfy the schema fails fast with FailureKindSchemaInvalid.
//
// Spec anchor: docs/product-spec/v0.8.0/05-multi-agent-layer.md.
type Handoff struct {
	ID                   string          `json:"id"`
	RunID                string          `json:"runId"`
	From                 string          `json:"from"`
	To                   string          `json:"to"`
	Reason               string          `json:"reason,omitempty"`
	Payload              json.RawMessage `json:"payload,omitempty"`
	EvidenceIDs          []string        `json:"evidenceIds,omitempty"`
	RequiredOutputSchema json.RawMessage `json:"requiredOutputSchema,omitempty"`
	CreatedAt            time.Time       `json:"createdAt"`
}
