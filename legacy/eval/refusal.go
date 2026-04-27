package eval

import "strings"

type RefusalResult struct {
	RefusedAppropriately bool     `json:"refusedAppropriately,omitempty"`
	RefusalDetected      bool     `json:"refusalDetected,omitempty"`
	HallucinationRisk    bool     `json:"hallucinationRisk,omitempty"`
	Signals              []string `json:"signals,omitempty"`
	Score                float64  `json:"score,omitempty"`
}

func DetectRefusal(answer string) RefusalResult {
	normalized := strings.ToLower(normalizeWhitespace(answer))
	result := RefusalResult{}
	for _, marker := range []string{
		"insufficient evidence",
		"not enough evidence",
		"cannot verify",
		"can't verify",
		"unable to verify",
		"the corpus does not contain",
		"i do not have enough evidence",
	} {
		if strings.Contains(normalized, marker) {
			result.RefusalDetected = true
			result.Signals = append(result.Signals, marker)
		}
	}
	if result.RefusalDetected {
		result.RefusedAppropriately = true
		result.Score = 1
		return result
	}
	if normalized != "" {
		result.HallucinationRisk = true
		result.Score = 0
	}
	return result
}

func refusalSignal(text string) bool {
	return DetectRefusal(text).RefusalDetected
}
