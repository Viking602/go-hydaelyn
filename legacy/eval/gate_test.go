package eval

import (
	"encoding/json"
	"testing"
)

func TestReleaseGate(t *testing.T) {
	tests := []struct {
		name     string
		score    *ScorePayload
		expected ReleaseDecision
	}{
		{
			name:     "nil score returns NoGo",
			score:    nil,
			expected: ReleaseDecisionNoGo,
		},
		{
			name: "A0 level returns NoGo",
			score: &ScorePayload{
				Level:            ScoreLevelA0,
				ReplayConsistent: true,
			},
			expected: ReleaseDecisionNoGo,
		},
		{
			name: "A1 level returns NoGo",
			score: &ScorePayload{
				Level:            ScoreLevelA1,
				ReplayConsistent: true,
			},
			expected: ReleaseDecisionNoGo,
		},
		{
			name: "A2 level returns ConditionalGo",
			score: &ScorePayload{
				Level:            ScoreLevelA2,
				ReplayConsistent: true,
			},
			expected: ReleaseDecisionConditional,
		},
		{
			name: "A3 level with consistent replay returns Go",
			score: &ScorePayload{
				Level:            ScoreLevelA3,
				ReplayConsistent: true,
			},
			expected: ReleaseDecisionGo,
		},
		{
			name: "A4 level with consistent replay returns Go",
			score: &ScorePayload{
				Level:            ScoreLevelA4,
				ReplayConsistent: true,
			},
			expected: ReleaseDecisionGo,
		},
		{
			name: "A3 level with inconsistent replay returns ConditionalGo",
			score: &ScorePayload{
				Level:            ScoreLevelA3,
				ReplayConsistent: false,
			},
			expected: ReleaseDecisionConditional,
		},
		{
			name: "A4 level with inconsistent replay returns ConditionalGo",
			score: &ScorePayload{
				Level:            ScoreLevelA4,
				ReplayConsistent: false,
			},
			expected: ReleaseDecisionConditional,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateReleaseGate(tt.score)
			if got != tt.expected {
				t.Errorf("EvaluateReleaseGate() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestReleaseGateRejectsCriticalSafetyFailure(t *testing.T) {
	// Any level with critical safety failure should be NoGo
	levels := []ScoreLevel{ScoreLevelA0, ScoreLevelA1, ScoreLevelA2, ScoreLevelA3, ScoreLevelA4}

	for _, level := range levels {
		score := &ScorePayload{
			Level:            level,
			ReplayConsistent: true,
			SafetyMetrics: &ScoreSafetyMetrics{
				CriticalFailure: true,
			},
		}

		got := EvaluateReleaseGate(score)
		if got != ReleaseDecisionNoGo {
			t.Errorf("EvaluateReleaseGate() with critical failure at level %s = %v, want NoGo", level, got)
		}
	}
}

func TestReleaseGateWithOutput(t *testing.T) {
	score := &ScorePayload{
		RunID:            "test-run-123",
		OverallScore:     0.85,
		Level:            ScoreLevelA3,
		ReplayConsistent: true,
		SafetyMetrics: &ScoreSafetyMetrics{
			CriticalFailure: false,
		},
	}

	output := EvaluateReleaseGateWithOutput(score)

	if output.Decision != ReleaseDecisionGo {
		t.Errorf("Expected Go decision, got %v", output.Decision)
	}
	if output.Level != ScoreLevelA3 {
		t.Errorf("Expected level A3, got %v", output.Level)
	}
	if output.Score != 0.85 {
		t.Errorf("Expected score 0.85, got %f", output.Score)
	}
	if !output.ReplayConsistent {
		t.Error("Expected replay consistent")
	}
	if output.SafetyCritical {
		t.Error("Expected no safety critical")
	}
	if len(output.Reasons) == 0 {
		t.Error("Expected at least one reason")
	}
	if len(output.Metadata) == 0 {
		t.Error("Expected metadata")
	}

	// Test JSON serialization
	jsonBytes, err := output.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	var decoded ReleaseGateOutput
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Fatalf("JSON unmarshal error = %v", err)
	}

	if decoded.Decision != output.Decision {
		t.Errorf("JSON round-trip decision mismatch")
	}
}

func TestReleaseGateOutputIsPassing(t *testing.T) {
	tests := []struct {
		name     string
		decision ReleaseDecision
		passing  bool
		blocking bool
	}{
		{"Go is passing", ReleaseDecisionGo, true, false},
		{"ConditionalGo is passing", ReleaseDecisionConditional, true, false},
		{"NoGo is blocking", ReleaseDecisionNoGo, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := &ReleaseGateOutput{Decision: tt.decision}
			if got := output.IsPassing(); got != tt.passing {
				t.Errorf("IsPassing() = %v, want %v", got, tt.passing)
			}
			if got := output.IsBlocking(); got != tt.blocking {
				t.Errorf("IsBlocking() = %v, want %v", got, tt.blocking)
			}
		})
	}
}

func TestEvaluateWithCriteria(t *testing.T) {
	tests := []struct {
		name     string
		score    *ScorePayload
		criteria *GateCriteria
		expected ReleaseDecision
	}{
		{
			name:     "nil score returns NoGo",
			score:    nil,
			criteria: DefaultGateCriteria(),
			expected: ReleaseDecisionNoGo,
		},
		{
			name:     "nil criteria returns NoGo",
			score:    &ScorePayload{Level: ScoreLevelA3},
			criteria: nil,
			expected: ReleaseDecisionNoGo,
		},
		{
			name: "custom min level A2 - A2 passes",
			score: &ScorePayload{
				Level:            ScoreLevelA2,
				ReplayConsistent: true,
			},
			criteria: &GateCriteria{
				MinLevel:                ScoreLevelA2,
				RequireReplayConsistent: true,
				BlockOnSafetyCritical:   true,
				MinScore:                0.0,
			},
			expected: ReleaseDecisionGo,
		},
		{
			name: "custom min level A2 - A1 fails",
			score: &ScorePayload{
				Level:            ScoreLevelA1,
				ReplayConsistent: true,
			},
			criteria: &GateCriteria{
				MinLevel:                ScoreLevelA2,
				RequireReplayConsistent: true,
				BlockOnSafetyCritical:   true,
				MinScore:                0.0,
			},
			expected: ReleaseDecisionNoGo,
		},
		{
			name: "score below min threshold returns Conditional",
			score: &ScorePayload{
				Level:            ScoreLevelA3,
				OverallScore:     0.5,
				ReplayConsistent: true,
			},
			criteria: &GateCriteria{
				MinLevel:                ScoreLevelA3,
				RequireReplayConsistent: true,
				BlockOnSafetyCritical:   true,
				MinScore:                0.7,
			},
			expected: ReleaseDecisionConditional,
		},
		{
			name: "replay not required - inconsistent still Go",
			score: &ScorePayload{
				Level:            ScoreLevelA3,
				ReplayConsistent: false,
			},
			criteria: &GateCriteria{
				MinLevel:                ScoreLevelA3,
				RequireReplayConsistent: false,
				BlockOnSafetyCritical:   true,
				MinScore:                0.0,
			},
			expected: ReleaseDecisionGo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateWithCriteria(tt.score, tt.criteria)
			if got != tt.expected {
				t.Errorf("EvaluateWithCriteria() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDefaultGateCriteria(t *testing.T) {
	criteria := DefaultGateCriteria()

	if criteria.MinLevel != ScoreLevelA3 {
		t.Errorf("Default min level = %v, want A3", criteria.MinLevel)
	}
	if !criteria.RequireReplayConsistent {
		t.Error("Default should require replay consistency")
	}
	if !criteria.BlockOnSafetyCritical {
		t.Error("Default should block on safety critical")
	}
	if criteria.MinScore != 0.7 {
		t.Errorf("Default min score = %f, want 0.7", criteria.MinScore)
	}
}

func TestReleaseGateOutputNilScore(t *testing.T) {
	output := EvaluateReleaseGateWithOutput(nil)

	if output.Decision != ReleaseDecisionNoGo {
		t.Errorf("Expected NoGo for nil score, got %v", output.Decision)
	}
	if len(output.Reasons) == 0 {
		t.Error("Expected reasons for nil score")
	}
	if output.Metadata["error"] != "nil score payload" {
		t.Error("Expected error metadata for nil score")
	}
}
