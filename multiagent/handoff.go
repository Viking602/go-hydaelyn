package multiagent

import (
	"encoding/json"
	"time"
)

// Handoff is the typed vocabulary multiagent schedulers will use to move work
// from one AgentInstance to another. Distinct from api.HandoffRequest (the
// durable runner's persistence shape) — Handoff is the in-memory scheduler-
// side value. v0.8.0 ships the type and store contracts; reference schedulers
// do not yet emit or persist Handoffs automatically. That wiring is reserved
// for v0.9.0.
//
// RequiredOutputSchema is the schema the receiving AgentInstance is expected
// to honor through its OutputPolicy. It becomes load-bearing once the v0.9.0
// Handoff flow wires scheduler validation and HandoffStore persistence.
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
