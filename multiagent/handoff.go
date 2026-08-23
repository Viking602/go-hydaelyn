package multiagent

import (
	"encoding/json"
	"time"

	"github.com/Viking602/venat/api"
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

// Record maps this scheduler-side handoff onto the durable store row.
// Spec anchor: ADR-026.
func (h Handoff) Record() api.HandoffRecord {
	return api.HandoffRecord{
		ID:                   h.ID,
		RunID:                h.RunID,
		From:                 h.From,
		To:                   h.To,
		Reason:               h.Reason,
		Payload:              append(json.RawMessage(nil), h.Payload...),
		EvidenceIDs:          append([]string(nil), h.EvidenceIDs...),
		RequiredOutputSchema: append(json.RawMessage(nil), h.RequiredOutputSchema...),
		CreatedAt:            h.CreatedAt,
	}
}

// HandoffFromRecord reconstructs a scheduler-side handoff from a store row.
// Spec anchor: ADR-026.
func HandoffFromRecord(record api.HandoffRecord) Handoff {
	return Handoff{
		ID:                   record.ID,
		RunID:                record.RunID,
		From:                 record.From,
		To:                   record.To,
		Reason:               record.Reason,
		Payload:              append(json.RawMessage(nil), record.Payload...),
		EvidenceIDs:          append([]string(nil), record.EvidenceIDs...),
		RequiredOutputSchema: append(json.RawMessage(nil), record.RequiredOutputSchema...),
		CreatedAt:            record.CreatedAt,
	}
}
