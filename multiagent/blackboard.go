package multiagent

import (
	"encoding/json"
	"time"
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
