package evaluation

import "time"

const ArtifactManifestSchemaVersion = "1.0"

type RetentionClass string

const (
	RetentionClassShortTerm RetentionClass = "short_term"
	RetentionClassLongTerm  RetentionClass = "long_term"
	RetentionClassPermanent RetentionClass = "permanent"
)

type ArtifactManifestKind string

const (
	ArtifactManifestKindEvents           ArtifactManifestKind = "events"
	ArtifactManifestKindReplayState      ArtifactManifestKind = "replay_state"
	ArtifactManifestKindAnswer           ArtifactManifestKind = "answer"
	ArtifactManifestKindToolCalls        ArtifactManifestKind = "tool_calls"
	ArtifactManifestKindModelEvents      ArtifactManifestKind = "model_events"
	ArtifactManifestKindEvaluationReport ArtifactManifestKind = "evaluation_report"
	ArtifactManifestKindScore            ArtifactManifestKind = "score"
	ArtifactManifestKindSummary          ArtifactManifestKind = "summary"
	ArtifactManifestKindPolicyOutcomes   ArtifactManifestKind = "policy_outcomes"
)

type ArtifactManifest struct {
	SchemaVersion string                  `json:"schemaVersion"`
	RunID         string                  `json:"runId"`
	CreatedAt     time.Time               `json:"createdAt"`
	Entries       []ArtifactManifestEntry `json:"entries,omitempty"`
}

type ArtifactManifestEntry struct {
	ID             string               `json:"id"`
	Kind           ArtifactManifestKind `json:"kind"`
	Path           string               `json:"path,omitempty"`
	URI            string               `json:"uri,omitempty"`
	Checksum       string               `json:"checksum,omitempty"`
	Size           int64                `json:"size,omitempty"`
	ContentType    string               `json:"contentType,omitempty"`
	RetentionClass RetentionClass       `json:"retentionClass,omitempty"`
	Redacted       bool                 `json:"redacted,omitempty"`
}
