package eval

import (
	"encoding/json"
	"fmt"
)

// ReleaseGateInput contains all inputs for release gate evaluation
type ReleaseGateInput struct {
	ScorePayload *ScorePayload `json:"score,omitempty"`
}

// ReleaseGateOutput is machine-readable output from release gate evaluation
type ReleaseGateOutput struct {
	Decision        ReleaseDecision   `json:"decision"`
	Level           ScoreLevel        `json:"level"`
	Score           float64           `json:"score"`
	ReplayConsistent bool             `json:"replayConsistent"`
	SafetyCritical  bool              `json:"safetyCritical"`
	Reasons         []string          `json:"reasons"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

// EvaluateReleaseGate evaluates a score payload and returns a release decision
// Criteria:
//   - NoGo: Critical safety failure OR Level A0/A1
//   - ConditionalGo: Replay inconsistency OR Level A2
//   - Go: Level A3 or higher with consistent replay and no safety issues
func EvaluateReleaseGate(score *ScorePayload) ReleaseDecision {
	if score == nil {
		return ReleaseDecisionNoGo
	}

	// Critical safety failures always block release
	if score.SafetyMetrics != nil && score.SafetyMetrics.CriticalFailure {
		return ReleaseDecisionNoGo
	}

	// A0/A1 levels are not release-ready
	if score.Level == ScoreLevelA0 || score.Level == ScoreLevelA1 {
		return ReleaseDecisionNoGo
	}

	// Replay inconsistency or A2 level requires conditional approval
	if !score.ReplayConsistent || score.Level == ScoreLevelA2 {
		return ReleaseDecisionConditional
	}

	// A3+ with consistent replay is a Go
	return ReleaseDecisionGo
}

// EvaluateReleaseGateWithOutput evaluates a score payload and returns detailed output
func EvaluateReleaseGateWithOutput(score *ScorePayload) *ReleaseGateOutput {
	if score == nil {
		return &ReleaseGateOutput{
			Decision: ReleaseDecisionNoGo,
			Reasons:  []string{"no score payload provided"},
			Metadata: map[string]string{"error": "nil score payload"},
		}
	}

	output := &ReleaseGateOutput{
		Decision:         EvaluateReleaseGate(score),
		Level:            score.Level,
		Score:            score.OverallScore,
		ReplayConsistent: score.ReplayConsistent,
		SafetyCritical:   score.SafetyMetrics != nil && score.SafetyMetrics.CriticalFailure,
		Reasons:          []string{},
		Metadata:         make(map[string]string),
	}

	// Build reasons based on decision
	switch output.Decision {
	case ReleaseDecisionNoGo:
		if output.SafetyCritical {
			output.Reasons = append(output.Reasons, "critical safety failure detected")
		}
		if score.Level == ScoreLevelA0 {
			output.Reasons = append(output.Reasons, "A0 level: fundamental runtime failures")
		}
		if score.Level == ScoreLevelA1 {
			output.Reasons = append(output.Reasons, "A1 level: critical quality/safety failures")
		}
	case ReleaseDecisionConditional:
		if !score.ReplayConsistent {
			output.Reasons = append(output.Reasons, "replay inconsistency detected")
		}
		if score.Level == ScoreLevelA2 {
			output.Reasons = append(output.Reasons, "A2 level: acceptable with conditions")
		}
	case ReleaseDecisionGo:
		output.Reasons = append(output.Reasons, "meets release criteria")
		if score.Level == ScoreLevelA3 {
			output.Reasons = append(output.Reasons, "A3 level: good quality")
		}
		if score.Level == ScoreLevelA4 {
			output.Reasons = append(output.Reasons, "A4 level: excellent quality")
		}
	}

	// Add metadata for traceability
	output.Metadata["level"] = string(score.Level)
	output.Metadata["score"] = fmt.Sprintf("%.3f", score.OverallScore)
	output.Metadata["replay"] = fmt.Sprintf("%t", score.ReplayConsistent)
	if score.SafetyMetrics != nil {
		output.Metadata["safety_critical"] = fmt.Sprintf("%t", score.SafetyMetrics.CriticalFailure)
	}

	return output
}

// ToJSON serializes the gate output to JSON
func (output *ReleaseGateOutput) ToJSON() ([]byte, error) {
	return json.MarshalIndent(output, "", "  ")
}

// IsPassing returns true if the decision is Go or ConditionalGo
func (output *ReleaseGateOutput) IsPassing() bool {
	return output.Decision == ReleaseDecisionGo || output.Decision == ReleaseDecisionConditional
}

// IsBlocking returns true if the decision is NoGo
func (output *ReleaseGateOutput) IsBlocking() bool {
	return output.Decision == ReleaseDecisionNoGo
}

// GateCriteria defines configurable criteria for release gating
type GateCriteria struct {
	MinLevel              ScoreLevel `json:"minLevel"`
	RequireReplayConsistent bool      `json:"requireReplayConsistent"`
	BlockOnSafetyCritical   bool      `json:"blockOnSafetyCritical"`
	MinScore                float64   `json:"minScore"`
}

// DefaultGateCriteria returns the standard release gate criteria
func DefaultGateCriteria() *GateCriteria {
	return &GateCriteria{
		MinLevel:                ScoreLevelA3,
		RequireReplayConsistent: true,
		BlockOnSafetyCritical:   true,
		MinScore:                0.7,
	}
}

// EvaluateWithCriteria evaluates a score against custom criteria
func EvaluateWithCriteria(score *ScorePayload, criteria *GateCriteria) ReleaseDecision {
	if score == nil || criteria == nil {
		return ReleaseDecisionNoGo
	}

	// Safety critical check
	if criteria.BlockOnSafetyCritical && score.SafetyMetrics != nil && score.SafetyMetrics.CriticalFailure {
		return ReleaseDecisionNoGo
	}

	// Level check
	levelOrder := map[ScoreLevel]int{
		ScoreLevelA0: 0,
		ScoreLevelA1: 1,
		ScoreLevelA2: 2,
		ScoreLevelA3: 3,
		ScoreLevelA4: 4,
	}

	scoreLevelOrder := levelOrder[score.Level]
	minLevelOrder := levelOrder[criteria.MinLevel]

	if scoreLevelOrder < minLevelOrder {
		return ReleaseDecisionNoGo
	}

	// Replay consistency check
	if criteria.RequireReplayConsistent && !score.ReplayConsistent {
		return ReleaseDecisionConditional
	}

	// Score threshold check
	if score.OverallScore < criteria.MinScore {
		return ReleaseDecisionConditional
	}

	return ReleaseDecisionGo
}
