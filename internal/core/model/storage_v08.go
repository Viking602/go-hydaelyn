package model

import (
	"encoding/json"
	"time"
)

// RunSelector mirrors api.RunSelector. AND-combined fields.
type RunSelector struct {
	IDs          []string
	AgentID      string
	AgentVersion string
	Statuses     []RunStatus
	Since        time.Time
	Until        time.Time
	Limit        int
}

// UserMessageSelector mirrors api.UserMessageSelector.
type UserMessageSelector struct {
	RunID     string
	Recipient string
	Statuses  []string
	Since     time.Time
	Until     time.Time
	Limit     int
}

// ResumeTokenSelector mirrors api.ResumeTokenSelector.
type ResumeTokenSelector struct {
	RunID    string
	TaskID   string
	Statuses []string
	Since    time.Time
	Until    time.Time
	Limit    int
	Cursor   string
}

// AgentSelector mirrors api.AgentSelector.
type AgentSelector struct {
	IDs      []string
	Roles    []string
	Groups   []string
	Statuses []string
	Limit    int
}

// CapabilitySelector mirrors api.CapabilitySelector.
type CapabilitySelector struct {
	Names    []string
	AgentIDs []string
	Tags     []string
	Limit    int
}

// UsageRecord mirrors api.UsageRecord. Append-only metering datum.
type UsageKind string

const (
	UsageKindModelCall       UsageKind = "model_call"
	UsageKindToolCall        UsageKind = "tool_call"
	UsageKindActionCall      UsageKind = "action_call"
	UsageKindContext         UsageKind = "context"
	UsageKindLegacyExecution UsageKind = "legacy_execution"
)

type UsagePricingState string

const (
	UsagePricingStatePriced   UsagePricingState = "priced"
	UsagePricingStateUnpriced UsagePricingState = "unpriced"
)

type UsageRecord struct {
	PricingState          UsagePricingState
	ID                    string
	RunID                 string
	TaskID                string
	AgentID               string
	Kind                  UsageKind
	Provider              string
	Model                 string
	ToolName              string
	InputTokens           int
	OutputTokens          int
	CachedInputTokens     int
	CacheWriteInputTokens int
	TotalTokens           int
	ToolCalls             int
	Steps                 int
	DurationMS            int64
	Credits               int64
	CreditsKind           string
	Metadata              map[string]string
	CreatedAt             time.Time
}

// UsageSelector mirrors api.UsageSelector.
type UsageSelector struct {
	RunID    string
	TaskID   string
	AgentID  string
	Kind     UsageKind
	Provider string
	ToolName string
	Since    time.Time
	Until    time.Time
	Limit    int
}

// DeadLetterEntry mirrors api.DeadLetterEntry.
type DeadLetterEntry struct {
	ID         string
	EnvelopeID string
	RunID      string
	TaskID     string
	Reason     string
	Attempts   int
	Envelope   TaskEnvelope
	Payload    map[string]any
	CreatedAt  time.Time
}

// DeadLetterSelector mirrors api.DeadLetterSelector.
type DeadLetterSelector struct {
	RunID  string
	TaskID string
	Since  time.Time
	Until  time.Time
	Limit  int
}

// Capability mirrors api.Capability. Declaration of a single unit of work
// an agent exposes to the runtime.
type Capability struct {
	Name             string
	Version          string
	Description      string
	AgentID          string
	InputSchema      map[string]any
	OutputSchema     map[string]any
	EffectType       ToolEffectType
	RiskLevel        string
	Idempotent       bool
	RequiresApproval bool
	Tags             []string
	Metadata         map[string]string
}

// HandoffRecord mirrors api.HandoffRecord — the durable twin of a
// multiagent typed handoff. Append-only; (RunID, ID) is the unique key.
type HandoffRecord struct {
	ID                   string
	RunID                string
	From                 string
	To                   string
	Reason               string
	Payload              json.RawMessage
	EvidenceIDs          []string
	RequiredOutputSchema json.RawMessage
	CreatedAt            time.Time
}

// HandoffSelector mirrors api.HandoffSelector. AND-combined fields.
type HandoffSelector struct {
	RunID string
	From  string
	To    string
	Since time.Time
}

// TeamStateRecord mirrors api.TeamStateRecord — the latest scheduler
// snapshot per run; the audit trail stays in the event log.
type TeamStateRecord struct {
	RunID     string
	Tick      int
	State     json.RawMessage
	UpdatedAt time.Time
}

// AgentInstanceRecord mirrors api.AgentInstanceRecord.
type AgentInstanceRecord struct {
	ID        string
	ClassName string
	RunID     string
	TaskID    string
	State     string
	CreatedAt time.Time
}

// AgentInstanceSelector mirrors api.AgentInstanceSelector. AND-combined.
type AgentInstanceSelector struct {
	RunID     string
	ClassName string
	State     string
}
