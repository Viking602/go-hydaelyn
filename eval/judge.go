package eval

type JudgeConfig struct {
	Enabled              bool                                                `json:"enabled,omitempty"`
	MinGroundedness      float64                                             `json:"minGroundedness,omitempty"`
	RequireValidCitations bool                                               `json:"requireValidCitations,omitempty"`
	Judge                func(answer string, corpus Corpus) (JudgeVerdict, error) `json:"-"`
}

type JudgeVerdict struct {
	Score     float64 `json:"score,omitempty"`
	Pass      bool    `json:"pass,omitempty"`
	Summary   string  `json:"summary,omitempty"`
	ModelName string  `json:"modelName,omitempty"`
}

type JudgeResult struct {
	Invoked               bool         `json:"invoked,omitempty"`
	ManifestRecorded      bool         `json:"manifestRecorded,omitempty"`
	Passed                bool         `json:"passed,omitempty"`
	DeterministicPass     bool         `json:"deterministicPass,omitempty"`
	DeterministicFailures []string     `json:"deterministicFailures,omitempty"`
	Groundedness          GroundednessResult `json:"groundedness,omitempty"`
	Citations             CitationResult     `json:"citations,omitempty"`
	Refusal               RefusalResult      `json:"refusal,omitempty"`
	Verdict               *JudgeVerdict `json:"verdict,omitempty"`
	Error                 string       `json:"error,omitempty"`
}

func JudgeWithLLM(answer string, corpus Corpus, judgeConfig JudgeConfig) JudgeResult {
	groundedness := CheckGroundedness(answer, corpus)
	citations := ValidateCitations(answer, corpus)
	refusal := DetectRefusal(answer)

	result := JudgeResult{
		Groundedness: groundedness,
		Citations:    citations,
		Refusal:      refusal,
	}

	if groundedness.UnsupportedClaims > 0 {
		result.DeterministicFailures = append(result.DeterministicFailures, "unsupported_claims")
	}
	if judgeConfig.MinGroundedness > 0 && groundedness.GroundednessRatio < judgeConfig.MinGroundedness {
		result.DeterministicFailures = append(result.DeterministicFailures, "groundedness_below_threshold")
	}
	if judgeConfig.RequireValidCitations && len(citations.InvalidCitations) > 0 {
		result.DeterministicFailures = append(result.DeterministicFailures, "invalid_citations")
	}
	if refusal.HallucinationRisk && groundedness.UnsupportedClaims > 0 {
		result.DeterministicFailures = append(result.DeterministicFailures, "missing_evidence_without_refusal")
	}

	result.DeterministicPass = len(result.DeterministicFailures) == 0
	result.Passed = result.DeterministicPass

	if !judgeConfig.Enabled || judgeConfig.Judge == nil {
		return result
	}

	result.Invoked = true
		result.ManifestRecorded = true
	verdict, err := judgeConfig.Judge(answer, corpus)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Verdict = &verdict
	if result.DeterministicPass {
		result.Passed = verdict.Pass
	}
	return result
}
