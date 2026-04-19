package evaluation

import (
	"reflect"
	"testing"
	"time"
)

func TestAdaptReportToScorePayload(t *testing.T) {
	t.Parallel()

	report := Report{
		TeamID:                 "team-123",
		TaskCompletionRate:     0.9,
		BlockingFailureRate:    0.1,
		RetrySuccessRate:       0.8,
		SupportedClaimRatio:    0.7,
		SynthesisInputCoverage: 0.6,
		EndToEndLatency:        1500 * time.Millisecond,
		ToolCallCount:          4,
		TokenBudgetHitRate:     0.2,
	}

	got := AdaptReportToScorePayload(report, "")
	want := ScorePayload{
		SchemaVersion:    ScorePayloadSchemaVersion,
		RunID:            "team-123",
		OverallScore:     0.7833333333333333,
		Level:            ScoreLevelA2,
		ReplayConsistent: true,
		RuntimeMetrics: &ScoreRuntimeMetrics{
			TaskCompletionRate:  0.9,
			BlockingFailureRate: 0.1,
			RetrySuccessRate:    0.8,
			EndToEndLatencyMs:   1500,
			ToolCallCount:       4,
			TokenBudgetHitRate:  0.2,
		},
		QualityMetrics: &ScoreQualityMetrics{
			Groundedness:           0.7,
			SynthesisInputCoverage: 0.6,
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("adapted payload mismatch\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestScoreLevelForOverallScore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		score float64
		want  ScoreLevel
	}{
		{name: "a0", score: 0.49, want: ScoreLevelA0},
		{name: "a1", score: 0.50, want: ScoreLevelA1},
		{name: "a2", score: 0.65, want: ScoreLevelA2},
		{name: "a3", score: 0.80, want: ScoreLevelA3},
		{name: "a4", score: 0.90, want: ScoreLevelA4},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ScoreLevelForOverallScore(tc.score); got != tc.want {
				t.Fatalf("ScoreLevelForOverallScore(%v) = %s, want %s", tc.score, got, tc.want)
			}
		})
	}
}

func TestScoreLevelForOverallScoreWithReplayConsistency(t *testing.T) {
	t.Parallel()

	if got := ScoreLevelForOverallScoreWithReplayConsistency(0.95, false); got != ScoreLevelA2 {
		t.Fatalf("expected replay inconsistency to cap score level at A2, got %s", got)
	}
}
