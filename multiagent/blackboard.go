package multiagent

import (
	"encoding/json"
	"time"

	"github.com/Viking602/venat/api"
)

// BlackboardEntry is a multi-agent-scoped Blackboard write. It extends
// the runner-scoped api.BlackboardItem with provenance fields required
// for typed cross-agent collaboration: WrittenBy is the AgentInstance
// that produced the entry, StepID ties it back to one agent.Step in the
// loop trace, and EvidenceID lets supervisor / voting schedulers
// reason about the chain of evidence that led to a decision.
//
// Spec anchor: docs/product-spec/v0.8.0/05-multi-agent-layer.md.
type BlackboardEntry struct {
	Key        string          `json:"key"`
	Value      json.RawMessage `json:"value"`
	WrittenBy  string          `json:"writtenBy"`
	StepID     string          `json:"stepId,omitempty"`
	EvidenceID string          `json:"evidenceId,omitempty"`
	CreatedAt  time.Time       `json:"createdAt"`
}

// Item maps this scheduler-side entry onto the durable Blackboard row.
// WrittenBy becomes SourceAgent; EvidenceID becomes EvidenceRefs.
// Spec anchor: ADR-026.
func (e BlackboardEntry) Item(runID string) api.BlackboardItem {
	item := api.BlackboardItem{
		ID:         e.Key,
		RunID:      runID,
		Type:       api.BlackboardItemTaskOutput,
		Source:     api.SourceIdentity{Type: api.SourceAgent, ID: e.WrittenBy},
		Key:        e.Key,
		Payload:    string(e.Value),
		Visibility: api.BlackboardVisibilityAgentVisible,
		CreatedAt:  e.CreatedAt,
	}
	if e.EvidenceID != "" {
		item.EvidenceRefs = []string{e.EvidenceID}
	}
	return item
}

// EntryFromItem reconstructs a scheduler-side entry from a durable row.
// Spec anchor: ADR-026.
func EntryFromItem(item api.BlackboardItem) BlackboardEntry {
	entry := BlackboardEntry{
		Key:       item.Key,
		Value:     json.RawMessage(item.Payload),
		WrittenBy: item.Source.ID,
		CreatedAt: item.CreatedAt,
	}
	if entry.Key == "" {
		entry.Key = item.ID
	}
	if len(item.EvidenceRefs) > 0 {
		entry.EvidenceID = item.EvidenceRefs[0]
	}
	return entry
}
