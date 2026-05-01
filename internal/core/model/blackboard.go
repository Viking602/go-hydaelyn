package model

import "time"

type BlackboardVisibility string

const (
	BlackboardVisibilityInternal             BlackboardVisibility = "internal"
	BlackboardVisibilityAgentVisible         BlackboardVisibility = "agent_visible"
	BlackboardVisibilityUserVisibleCandidate BlackboardVisibility = "user_visible_candidate"
	BlackboardVisibilityUserVisible          BlackboardVisibility = "user_visible"
)

// BlackboardItemType is the kind of evidence/output a blackboard entry carries.
// The framework writes only generic kinds (claim/evidence/finding/...) and
// leaves business-specific kinds to the developer; pass any string when the
// caller writes its own item.
type BlackboardItemType string

const (
	BlackboardItemClaim          BlackboardItemType = "claim"
	BlackboardItemEvidence       BlackboardItemType = "evidence"
	BlackboardItemFinding        BlackboardItemType = "finding"
	BlackboardItemArtifactRef    BlackboardItemType = "artifact_ref"
	BlackboardItemContext        BlackboardItemType = "context"
	BlackboardItemTaskOutput     BlackboardItemType = "task_output"
	BlackboardItemHandoffContext BlackboardItemType = "handoff_context"
)

type SourceType string

const (
	SourceAgent     SourceType = "agent"
	SourceComponent SourceType = "component"
	SourceTool      SourceType = "tool"
	SourceSystem    SourceType = "system"
)

type SourceIdentity struct {
	Type SourceType `json:"type"`
	ID   string     `json:"id"`
}

type BlackboardItem struct {
	ID           string               `json:"id"`
	RunID        string               `json:"runId"`
	TaskID       string               `json:"taskId,omitempty"`
	Type         BlackboardItemType   `json:"type,omitempty"`
	Source       SourceIdentity       `json:"source"`
	Content      string               `json:"content,omitempty"`
	Confidence   float64              `json:"confidence,omitempty"`
	EvidenceRefs []string             `json:"evidenceRefs,omitempty"`
	ArtifactRefs []string             `json:"artifactRefs,omitempty"`
	Visibility   BlackboardVisibility `json:"visibility"`
	Version      int                  `json:"version,omitempty"`
	Key          string               `json:"key,omitempty"`
	Payload      string               `json:"payload,omitempty"`
	CreatedAt    time.Time            `json:"createdAt"`
}

type BlackboardSelector struct {
	RunID       string               `json:"runId,omitempty"`
	TaskID      string               `json:"taskId,omitempty"`
	ItemTypes   []BlackboardItemType `json:"itemTypes,omitempty"`
	SourceTypes []SourceType         `json:"sourceTypes,omitempty"`
	SourceIDs   []string             `json:"sourceIds,omitempty"`
	// Deprecated: use SourceTypes: [SourceAgent] plus SourceIDs instead.
	SourceAgentIDs []string             `json:"sourceAgentIds,omitempty"`
	Visibility     BlackboardVisibility `json:"visibility,omitempty"`
	Tags           []string             `json:"tags,omitempty"`
	SinceVersion   int                  `json:"sinceVersion,omitempty"`
	Limit          int                  `json:"limit,omitempty"`
	Keys           []string             `json:"keys,omitempty"`
}
