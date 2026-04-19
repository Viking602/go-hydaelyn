package evaluation

const EvalCaseSchemaVersion = "1.0"

type EvalCase struct {
	SchemaVersion string              `json:"schemaVersion"`
	ID            string              `json:"id"`
	Suite         string              `json:"suite"`
	Pattern       string              `json:"pattern"`
	Input         map[string]any      `json:"input,omitempty"`
	Profiles      *EvalCaseProfiles   `json:"profiles,omitempty"`
	Tools         []string            `json:"tools,omitempty"`
	Fixtures      *EvalCaseFixtures   `json:"fixtures,omitempty"`
	Expected      *EvalCaseExpected   `json:"expected,omitempty"`
	Thresholds    *EvalCaseThresholds `json:"thresholds,omitempty"`
	Limits        *EvalCaseLimits     `json:"limits,omitempty"`
}

type EvalCaseProfiles struct {
	Supervisor string `json:"supervisor,omitempty"`
	Worker     string `json:"worker,omitempty"`
}

type EvalCaseFixtures struct {
	CorpusIDs []string `json:"corpusIds,omitempty"`
	Paths     []string `json:"paths,omitempty"`
}

type EvalCaseExpected struct {
	MustInclude       []string `json:"mustInclude,omitempty"`
	MustNotInclude    []string `json:"mustNotInclude,omitempty"`
	RequiredCitations []string `json:"requiredCitations,omitempty"`
}

type EvalCaseThresholds struct {
	TaskCompletionRate  float64 `json:"taskCompletionRate,omitempty"`
	Groundedness        float64 `json:"groundedness,omitempty"`
	SupportedClaimRatio float64 `json:"supportedClaimRatio,omitempty"`
	RetrySuccessRate    float64 `json:"retrySuccessRate,omitempty"`
}

type EvalCaseLimits struct {
	MaxToolCalls int `json:"maxToolCalls,omitempty"`
	MaxLatencyMs int `json:"maxLatencyMs,omitempty"`
	MaxTokens    int `json:"maxTokens,omitempty"`
}
