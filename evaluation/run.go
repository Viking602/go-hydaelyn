package evaluation

import "time"

const EvalRunSchemaVersion = "1.0"

type EvalRunMode string

const (
	EvalRunModeDeterministic EvalRunMode = "deterministic"
	EvalRunModeLive          EvalRunMode = "live"
)

type EvalRunStatus string

const (
	EvalRunStatusPending   EvalRunStatus = "pending"
	EvalRunStatusRunning   EvalRunStatus = "running"
	EvalRunStatusCompleted EvalRunStatus = "completed"
	EvalRunStatusFailed    EvalRunStatus = "failed"
)

type EvalRun struct {
	SchemaVersion     string                 `json:"schemaVersion"`
	ID                string                 `json:"id"`
	CaseID            string                 `json:"caseId"`
	Mode              EvalRunMode            `json:"mode"`
	RuntimeConfigHash string                 `json:"runtimeConfigHash,omitempty"`
	Seed              int64                  `json:"seed,omitempty"`
	StartedAt         time.Time              `json:"startedAt"`
	CompletedAt       time.Time              `json:"completedAt"`
	TraceRefs         *EvalRunTraceRefs      `json:"traceRefs,omitempty"`
	ArtifactRefs      *EvalRunArtifactRefs   `json:"artifactRefs,omitempty"`
	ScoreRef          *EvalRunRef            `json:"scoreRef,omitempty"`
	PolicyOutcomes    []EvalRunPolicyOutcome `json:"policyOutcomes,omitempty"`
	Status            EvalRunStatus          `json:"status"`
	Error             string                 `json:"error,omitempty"`
}

type EvalRunTraceRefs struct {
	Events      *EvalRunRef `json:"events,omitempty"`
	ModelEvents *EvalRunRef `json:"modelEvents,omitempty"`
}

type EvalRunArtifactRefs struct {
	Events *EvalRunRef `json:"events,omitempty"`
	Replay *EvalRunRef `json:"replay,omitempty"`
	Answer *EvalRunRef `json:"answer,omitempty"`
}

type EvalRunRef struct {
	ID   string `json:"id,omitempty"`
	Path string `json:"path,omitempty"`
	URI  string `json:"uri,omitempty"`
}

type EvalRunPolicyOutcome struct {
	Policy    string `json:"policy,omitempty"`
	Outcome   string `json:"outcome,omitempty"`
	Severity  string `json:"severity,omitempty"`
	Message   string `json:"message,omitempty"`
	Blocking  bool   `json:"blocking,omitempty"`
	Reference string `json:"reference,omitempty"`
}
