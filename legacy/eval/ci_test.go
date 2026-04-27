package eval

import (
	"testing"
	"time"
)

// TestDeterministicEval tests the deterministic evaluation lane
func TestDeterministicEval(t *testing.T) {
	// Create a mock eval run with deterministic characteristics
	run := &EvalRun{
		SchemaVersion: EvalRunSchemaVersion,
		ID:            "test-deterministic-run",
		CaseID:        "test-case-1",
		Mode:          EvalRunModeDeterministic,
		Seed:          42,
		Status:        EvalRunStatusCompleted,
		StartedAt:     time.Now().Add(-time.Minute),
		CompletedAt:   time.Now(),
		TraceRefs: &EvalRunTraceRefs{
			Events: &EvalRunRef{ID: "events-1", Path: "events.json"},
		},
	}

	// Create a complete score payload
	score := &ScorePayload{
		SchemaVersion:    ScorePayloadSchemaVersion,
		RunID:            run.ID,
		OverallScore:     0.82,
		Level:            ScoreLevelA3,
		ReplayConsistent: true,
		RuntimeMetrics: &ScoreRuntimeMetrics{
			TaskCompletionRate:  1.0,
			BlockingFailureRate: 0.0,
			RetrySuccessRate:    1.0,
			EndToEndLatencyMs:   1500,
			ToolCallCount:       3,
			TokenBudgetHitRate:  0.0,
		},
		QualityMetrics: &ScoreQualityMetrics{
			AnswerCorrectness:      0.85,
			Groundedness:           0.90,
			CitationPrecision:      0.88,
			CitationRecall:         0.82,
			ToolPrecision:          0.95,
			ToolRecall:             0.90,
			ToolArgAccuracy:        0.92,
			SynthesisInputCoverage: 0.85,
		},
		SafetyMetrics: &ScoreSafetyMetrics{
			CriticalFailure:         false,
			PromptInjectionBlocked:  true,
			UnauthorizedToolBlocked: true,
			SecretLeakBlocked:       true,
		},
		Failures:        []ScoreFailure{},
		Recommendations: []ScoreRecommendation{},
	}

	// Test that deterministic eval produces Go decision
	decision := EvaluateReleaseGate(score)
	if decision != ReleaseDecisionGo {
		t.Errorf("Deterministic eval with A3 should be Go, got %v", decision)
	}

	// Generate summary report
	summary := GenerateSummaryReport(run, score)
	if summary == "" {
		t.Error("Summary should not be empty")
	}

	// Verify summary contains expected sections
	expectedSections := []string{
		"EVALUATION SUMMARY REPORT",
		"RUN INFORMATION",
		"SCORE SUMMARY",
		"RUNTIME METRICS",
		"QUALITY METRICS",
		"SAFETY METRICS",
	}

	for _, section := range expectedSections {
		if !contains(summary, section) {
			t.Errorf("Summary missing section: %s", section)
		}
	}

	// Verify deterministic mode is reflected
	if !contains(summary, string(EvalRunModeDeterministic)) {
		t.Error("Summary should mention deterministic mode")
	}

	// Verify release gate shows Go
	if !contains(summary, "GO") {
		t.Error("Summary should show GO for passing deterministic eval")
	}
}

// TestSafetyCritical tests the safety critical evaluation lane
func TestSafetyCritical(t *testing.T) {
	tests := []struct {
		name          string
		safetyMetrics *ScoreSafetyMetrics
		expectedLevel ScoreLevel
		shouldBlock   bool
	}{
		{
			name: "all safety checks pass",
			safetyMetrics: &ScoreSafetyMetrics{
				CriticalFailure:         false,
				PromptInjectionBlocked:  true,
				UnauthorizedToolBlocked: true,
				SecretLeakBlocked:       true,
			},
			expectedLevel: ScoreLevelA3,
			shouldBlock:   false,
		},
		{
			name: "critical failure blocks release",
			safetyMetrics: &ScoreSafetyMetrics{
				CriticalFailure:         true,
				PromptInjectionBlocked:  true,
				UnauthorizedToolBlocked: true,
				SecretLeakBlocked:       true,
			},
			expectedLevel: ScoreLevelA1,
			shouldBlock:   true,
		},
		{
			name: "partial safety metrics",
			safetyMetrics: &ScoreSafetyMetrics{
				CriticalFailure: false,
			},
			expectedLevel: ScoreLevelA3,
			shouldBlock:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := &ScorePayload{
				RunID:            "test-safety-run",
				OverallScore:     0.85,
				Level:            tt.expectedLevel,
				ReplayConsistent: true,
				SafetyMetrics:    tt.safetyMetrics,
				RuntimeMetrics: &ScoreRuntimeMetrics{
					TaskCompletionRate: 1.0,
				},
				QualityMetrics: &ScoreQualityMetrics{
					Groundedness: 0.90,
				},
			}

			decision := EvaluateReleaseGate(score)

			if tt.shouldBlock && decision != ReleaseDecisionNoGo {
				t.Errorf("Safety critical failure should block, got %v", decision)
			}
			if !tt.shouldBlock && decision == ReleaseDecisionNoGo {
				t.Errorf("Safety check should pass, got %v", decision)
			}
		})
	}
}

// TestReplayInvariant tests the replay invariant evaluation lane
func TestReplayInvariant(t *testing.T) {
	tests := []struct {
		name             string
		replayConsistent bool
		level            ScoreLevel
		expectedDecision ReleaseDecision
	}{
		{
			name:             "consistent replay at A3",
			replayConsistent: true,
			level:            ScoreLevelA3,
			expectedDecision: ReleaseDecisionGo,
		},
		{
			name:             "inconsistent replay at A3",
			replayConsistent: false,
			level:            ScoreLevelA3,
			expectedDecision: ReleaseDecisionConditional,
		},
		{
			name:             "inconsistent replay at A4",
			replayConsistent: false,
			level:            ScoreLevelA4,
			expectedDecision: ReleaseDecisionConditional,
		},
		{
			name:             "consistent replay at A2",
			replayConsistent: true,
			level:            ScoreLevelA2,
			expectedDecision: ReleaseDecisionConditional,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := &ScorePayload{
				RunID:            "test-replay-run",
				OverallScore:     0.80,
				Level:            tt.level,
				ReplayConsistent: tt.replayConsistent,
				SafetyMetrics: &ScoreSafetyMetrics{
					CriticalFailure: false,
				},
				RuntimeMetrics: &ScoreRuntimeMetrics{
					TaskCompletionRate: 1.0,
				},
			}

			decision := EvaluateReleaseGate(score)
			if decision != tt.expectedDecision {
				t.Errorf("Replay invariant test: got %v, want %v", decision, tt.expectedDecision)
			}
		})
	}
}

// TestFullEvalSuite runs a complete evaluation suite combining all lanes
func TestFullEvalSuite(t *testing.T) {
	// Test case: Passing all lanes
	t.Run("passing all lanes", func(t *testing.T) {
		score := &ScorePayload{
			RunID:            "full-suite-pass",
			OverallScore:     0.88,
			Level:            ScoreLevelA4,
			ReplayConsistent: true,
			RuntimeMetrics: &ScoreRuntimeMetrics{
				TaskCompletionRate:  1.0,
				BlockingFailureRate: 0.0,
				RetrySuccessRate:    1.0,
				TokenBudgetHitRate:  0.0,
			},
			QualityMetrics: &ScoreQualityMetrics{
				AnswerCorrectness:      0.90,
				Groundedness:           0.92,
				CitationPrecision:      0.88,
				CitationRecall:         0.85,
				ToolPrecision:          0.95,
				ToolRecall:             0.90,
				ToolArgAccuracy:        0.93,
				SynthesisInputCoverage: 0.88,
			},
			SafetyMetrics: &ScoreSafetyMetrics{
				CriticalFailure:         false,
				PromptInjectionBlocked:  true,
				UnauthorizedToolBlocked: true,
				SecretLeakBlocked:       true,
			},
			Failures:        []ScoreFailure{},
			Recommendations: []ScoreRecommendation{},
		}

		decision := EvaluateReleaseGate(score)
		if decision != ReleaseDecisionGo {
			t.Errorf("Full passing suite should be Go, got %v", decision)
		}

		// Verify capability report
		report := GenerateCapabilityReport(score)
		if report == nil {
			t.Fatal("Capability report should not be nil")
		}
		if report.ReleaseDecision != ReleaseDecisionGo {
			t.Errorf("Report decision should be Go, got %v", report.ReleaseDecision)
		}
		if len(report.Radar) == 0 {
			t.Error("Radar should have dimensions")
		}
	})

	// Test case: Failing safety lane
	t.Run("failing safety lane", func(t *testing.T) {
		score := &ScorePayload{
			RunID:            "safety-fail",
			OverallScore:     0.85,
			Level:            ScoreLevelA3,
			ReplayConsistent: true,
			SafetyMetrics: &ScoreSafetyMetrics{
				CriticalFailure:         true,
				PromptInjectionBlocked:  true,
				UnauthorizedToolBlocked: true,
				SecretLeakBlocked:       true,
			},
		}

		decision := EvaluateReleaseGate(score)
		if decision != ReleaseDecisionNoGo {
			t.Errorf("Safety failure should be NoGo, got %v", decision)
		}

		report := GenerateCapabilityReport(score)
		if report.ReleaseDecision != ReleaseDecisionNoGo {
			t.Errorf("Report should show NoGo for safety failure")
		}
	})

	// Test case: Failing replay lane
	t.Run("failing replay lane", func(t *testing.T) {
		score := &ScorePayload{
			RunID:            "replay-fail",
			OverallScore:     0.85,
			Level:            ScoreLevelA3,
			ReplayConsistent: false,
			SafetyMetrics: &ScoreSafetyMetrics{
				CriticalFailure: false,
			},
		}

		decision := EvaluateReleaseGate(score)
		if decision != ReleaseDecisionConditional {
			t.Errorf("Replay failure should be Conditional, got %v", decision)
		}

		report := GenerateCapabilityReport(score)
		if report.ReplayConsistent {
			t.Error("Report should show replay inconsistent")
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
