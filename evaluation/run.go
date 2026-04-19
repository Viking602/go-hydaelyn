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
	Provenance        *EvalRunProvenance     `json:"provenance,omitempty"`
	Usage             *EvalRunUsage          `json:"usage,omitempty"`
	Variance          *EvalRunVariance       `json:"variance,omitempty"`
	Seed              int64                  `json:"seed,omitempty"`
	StartedAt         time.Time              `json:"startedAt"`
	CompletedAt       time.Time              `json:"completedAt"`
	ReplayConsistent  *bool                  `json:"replayConsistent,omitempty"`
	TraceRefs         *EvalRunTraceRefs      `json:"traceRefs,omitempty"`
	ArtifactRefs      *EvalRunArtifactRefs   `json:"artifactRefs,omitempty"`
	QualityMetrics    *ScoreQualityMetrics   `json:"qualityMetrics,omitempty"`
	ScoreRef          *EvalRunRef            `json:"scoreRef,omitempty"`
	PolicyOutcomes    []EvalRunPolicyOutcome `json:"policyOutcomes,omitempty"`
	Status            EvalRunStatus          `json:"status"`
	Error             string                 `json:"error,omitempty"`
}

type EvalRunProvenance struct {
	LaneID             string `json:"laneId,omitempty"`
	Provider           string `json:"provider,omitempty"`
	ProviderVersion    string `json:"providerVersion,omitempty"`
	ProviderProvenance string `json:"providerProvenance,omitempty"`
	Model              string `json:"model,omitempty"`
	ModelProvenance    string `json:"modelProvenance,omitempty"`
	ResolvedBaseURL    string `json:"resolvedBaseUrl,omitempty"`
}

type EvalRunUsage struct {
	PromptTokens     int     `json:"promptTokens,omitempty"`
	CompletionTokens int     `json:"completionTokens,omitempty"`
	TotalTokens      int     `json:"totalTokens,omitempty"`
	TotalCostUSD     float64 `json:"totalCostUsd,omitempty"`
	LatencyMs        int64   `json:"latencyMs,omitempty"`
	ToolCallCount    int     `json:"toolCallCount,omitempty"`
}

type EvalRunVariance struct {
	ComparedRunID   string             `json:"comparedRunId,omitempty"`
	Window          int                `json:"window,omitempty"`
	MetricDeltas    map[string]float64 `json:"metricDeltas,omitempty"`
	ExceededMetrics []string           `json:"exceededMetrics,omitempty"`
	WithinTolerance bool               `json:"withinTolerance,omitempty"`
}

type EvalRunTraceRefs struct {
	Events      *EvalRunRef `json:"events,omitempty"`
	ModelEvents *EvalRunRef `json:"modelEvents,omitempty"`
}

type EvalRunArtifactRefs struct {
	Events           *EvalRunRef `json:"events,omitempty"`
	Replay           *EvalRunRef `json:"replay,omitempty"`
	Answer           *EvalRunRef `json:"answer,omitempty"`
	FinalState       *EvalRunRef `json:"finalState,omitempty"`
	ReplayedState    *EvalRunRef `json:"replayedState,omitempty"`
	ToolCalls        *EvalRunRef `json:"toolCalls,omitempty"`
	ModelEvents      *EvalRunRef `json:"modelEvents,omitempty"`
	EvaluationReport *EvalRunRef `json:"evaluationReport,omitempty"`
	QualityScore     *EvalRunRef `json:"qualityScore,omitempty"`
	Summary          *EvalRunRef `json:"summary,omitempty"`
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
