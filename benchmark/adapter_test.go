package benchmark

import (
	"reflect"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/evaluation"
)

func TestAdaptScoreBundleToScorePayload(t *testing.T) {
	t.Parallel()

	bundle := ScoreBundle{
		Scores: map[string]float64{
			"overallScore":           0.7833333333333333,
			"taskCompletionRate":     0.9,
			"blockingFailureRate":    0.1,
			"retrySuccessRate":       0.8,
			"supportedClaimRatio":    0.7,
			"synthesisInputCoverage": 0.6,
			"tokenBudgetHitRate":     0.2,
			"toolCallCount":          4,
		},
		Cost: CostInfo{LatencyMs: 1500},
	}

	got := AdaptScoreBundleToScorePayload(bundle, "bench-123")
	want := evaluation.ScorePayload{
		SchemaVersion: evaluation.ScorePayloadSchemaVersion,
		RunID:         "bench-123",
		OverallScore:  0.7833333333333333,
		Level:         evaluation.ScoreLevelA2,
		RuntimeMetrics: &evaluation.ScoreRuntimeMetrics{
			TaskCompletionRate:  0.9,
			BlockingFailureRate: 0.1,
			RetrySuccessRate:    0.8,
			EndToEndLatencyMs:   1500,
			ToolCallCount:       4,
			TokenBudgetHitRate:  0.2,
		},
		QualityMetrics: &evaluation.ScoreQualityMetrics{
			Groundedness:           0.7,
			SynthesisInputCoverage: 0.6,
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("adapted payload mismatch\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestUnifiedScoreBundle(t *testing.T) {
	t.Parallel()

	report := evaluation.Report{
		TeamID:                 "shared-run",
		TaskCompletionRate:     0.9,
		BlockingFailureRate:    0.1,
		RetrySuccessRate:       0.8,
		SupportedClaimRatio:    0.7,
		SynthesisInputCoverage: 0.6,
		EndToEndLatency:        1500 * time.Millisecond,
		ToolCallCount:          4,
		TokenBudgetHitRate:     0.2,
	}
	runtimePayload := evaluation.AdaptReportToScorePayload(report, report.TeamID)
	benchmarkPayload := AdaptScoreBundleToScorePayload(ScoreBundle{
		Scores: map[string]float64{
			"overallScore":           runtimePayload.OverallScore,
			"taskCompletionRate":     0.9,
			"blockingFailureRate":    0.1,
			"retrySuccessRate":       0.8,
			"supportedClaimRatio":    0.7,
			"synthesisInputCoverage": 0.6,
			"tokenBudgetHitRate":     0.2,
			"toolCallCount":          4,
		},
		Cost: CostInfo{LatencyMs: 1500},
	}, report.TeamID)

	if !reflect.DeepEqual(benchmarkPayload, runtimePayload) {
		t.Fatalf("canonical payloads diverged\nruntime:   %#v\nbenchmark: %#v", runtimePayload, benchmarkPayload)
	}
}
