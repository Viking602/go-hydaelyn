package evaluation

import (
	"path/filepath"
	"strings"
)

func AdaptReportToScorePayload(report Report, runID string) ScorePayload {
	return AdaptReportToScorePayloadWithReplayConsistency(report, runID, true)
}

func AdaptReportToScorePayloadWithReplayConsistency(report Report, runID string, replayConsistent bool) ScorePayload {
	payload := ScorePayload{
		SchemaVersion:    ScorePayloadSchemaVersion,
		RunID:            canonicalRunID(runID, report.TeamID, "evaluation"),
		ReplayConsistent: replayConsistent,
		RuntimeMetrics: &ScoreRuntimeMetrics{
			TaskCompletionRate:  report.TaskCompletionRate,
			BlockingFailureRate: report.BlockingFailureRate,
			RetrySuccessRate:    report.RetrySuccessRate,
			EndToEndLatencyMs:   report.EndToEndLatency.Milliseconds(),
			ToolCallCount:       report.ToolCallCount,
			TokenBudgetHitRate:  report.TokenBudgetHitRate,
		},
	}

	if report.SupportedClaimRatio != 0 || report.SynthesisInputCoverage != 0 {
		payload.QualityMetrics = &ScoreQualityMetrics{
			Groundedness:           report.SupportedClaimRatio,
			SynthesisInputCoverage: report.SynthesisInputCoverage,
		}
	}

	payload.OverallScore = averageNormalizedScores([]float64{
		report.TaskCompletionRate,
		1 - report.BlockingFailureRate,
		report.RetrySuccessRate,
		report.SupportedClaimRatio,
		report.SynthesisInputCoverage,
		1 - report.TokenBudgetHitRate,
	})
	payload.Level = ScoreLevelForOverallScoreWithReplayConsistency(payload.OverallScore, payload.ReplayConsistent)
	return payload
}

func ScoreLevelForOverallScore(score float64) ScoreLevel {
	return ScoreLevelForOverallScoreWithReplayConsistency(score, true)
}

func ScoreLevelForOverallScoreWithReplayConsistency(score float64, replayConsistent bool) ScoreLevel {
	percentage := score * 100
	if !replayConsistent && percentage >= 80 {
		return ScoreLevelA2
	}
	switch {
	case percentage >= 90:
		return ScoreLevelA4
	case percentage >= 80:
		return ScoreLevelA3
	case percentage >= 65:
		return ScoreLevelA2
	case percentage >= 50:
		return ScoreLevelA1
	default:
		return ScoreLevelA0
	}
}

func canonicalRunID(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		base := filepath.Base(trimmed)
		ext := filepath.Ext(base)
		if ext != "" {
			base = strings.TrimSuffix(base, ext)
		}
		if base != "." && base != "" {
			return base
		}
		return trimmed
	}
	return "run"
}

func averageNormalizedScores(values []float64) float64 {
	total := 0.0
	count := 0
	for _, value := range values {
		if value < 0 {
			value = 0
		}
		if value > 1 {
			value = 1
		}
		total += value
		count++
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}
