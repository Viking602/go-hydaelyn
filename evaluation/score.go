package evaluation

const ScorePayloadSchemaVersion = "1.0"

type ScoreLevel string

const (
	ScoreLevelA0 ScoreLevel = "A0"
	ScoreLevelA1 ScoreLevel = "A1"
	ScoreLevelA2 ScoreLevel = "A2"
	ScoreLevelA3 ScoreLevel = "A3"
	ScoreLevelA4 ScoreLevel = "A4"
)

type ScorePayload struct {
	SchemaVersion    string                `json:"schemaVersion"`
	RunID            string                `json:"runId"`
	CaseID           string                `json:"caseId,omitempty"`
	Suite            string                `json:"suite,omitempty"`
	Pass             bool                  `json:"pass,omitempty"`
	OverallScore     float64               `json:"overallScore,omitempty"`
	Level            ScoreLevel            `json:"level,omitempty"`
	ReplayConsistent bool                  `json:"replayConsistent,omitempty"`
	RuntimeMetrics   *ScoreRuntimeMetrics  `json:"runtimeMetrics,omitempty"`
	QualityMetrics   *ScoreQualityMetrics  `json:"qualityMetrics,omitempty"`
	SafetyMetrics    *ScoreSafetyMetrics   `json:"safetyMetrics,omitempty"`
	Failures         []ScoreFailure        `json:"failures,omitempty"`
	Recommendations  []ScoreRecommendation `json:"recommendations,omitempty"`
}

type ScoreRuntimeMetrics struct {
	TaskCompletionRate  float64 `json:"taskCompletionRate,omitempty"`
	BlockingFailureRate float64 `json:"blockingFailureRate,omitempty"`
	RetrySuccessRate    float64 `json:"retrySuccessRate,omitempty"`
	EndToEndLatencyMs   int64   `json:"endToEndLatencyMs,omitempty"`
	ToolCallCount       int     `json:"toolCallCount,omitempty"`
	TotalTokens         int     `json:"totalTokens,omitempty"`
	TokenBudgetHitRate  float64 `json:"tokenBudgetHitRate,omitempty"`
}

type ScoreQualityMetrics struct {
	AnswerCorrectness      float64 `json:"answerCorrectness,omitempty"`
	Groundedness           float64 `json:"groundedness,omitempty"`
	CitationPrecision      float64 `json:"citationPrecision,omitempty"`
	CitationRecall         float64 `json:"citationRecall,omitempty"`
	ToolPrecision          float64 `json:"toolPrecision,omitempty"`
	ToolRecall             float64 `json:"toolRecall,omitempty"`
	ToolArgAccuracy        float64 `json:"toolArgAccuracy,omitempty"`
	SynthesisInputCoverage float64 `json:"synthesisInputCoverage,omitempty"`
}

type ScoreSafetyMetrics struct {
	CriticalFailure         bool `json:"criticalFailure,omitempty"`
	PromptInjectionBlocked  bool `json:"promptInjectionBlocked,omitempty"`
	UnauthorizedToolBlocked bool `json:"unauthorizedToolBlocked,omitempty"`
	SecretLeakBlocked       bool `json:"secretLeakBlocked,omitempty"`
}

type ScoreFailure struct {
	Code     string `json:"code,omitempty"`
	Message  string `json:"message,omitempty"`
	Metric   string `json:"metric,omitempty"`
	Layer    string `json:"layer,omitempty"`
	Severity string `json:"severity,omitempty"`
	Blocking bool   `json:"blocking,omitempty"`
}

type ScoreRecommendation struct {
	Priority  string `json:"priority,omitempty"`
	Action    string `json:"action,omitempty"`
	Rationale string `json:"rationale,omitempty"`
}
