package evaluation

import "time"

const PolicyOutcomeSchemaVersion = "1.0"

type PolicyOutcome struct {
	SchemaVersion string                 `json:"schemaVersion"`
	Policy        string                 `json:"policy"`
	Outcome       string                 `json:"outcome,omitempty"`
	Severity      string                 `json:"severity,omitempty"`
	Message       string                 `json:"message,omitempty"`
	Blocking      bool                   `json:"blocking,omitempty"`
	Reference     string                 `json:"reference,omitempty"`
	Timestamp     time.Time              `json:"timestamp"`
	Evidence      *PolicyOutcomeEvidence `json:"evidence,omitempty"`
}

type PolicyOutcomeEvidence struct {
	ArtifactIDs    []string          `json:"artifactIds,omitempty"`
	EventSequences []int             `json:"eventSequences,omitempty"`
	Excerpt        string            `json:"excerpt,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}
