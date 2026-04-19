package benchmark

import (
	"sort"

	"github.com/Viking602/go-hydaelyn/evaluation"
)

func AdaptScoreBundleToScorePayload(bundle ScoreBundle, runID string) evaluation.ScorePayload {
	payload := evaluation.ScorePayload{
		SchemaVersion:    evaluation.ScorePayloadSchemaVersion,
		RunID:            runID,
		ReplayConsistent: true,
	}

	runtimeMetrics := &evaluation.ScoreRuntimeMetrics{}
	qualityMetrics := &evaluation.ScoreQualityMetrics{}
	var runtimeSet, qualitySet bool

	metrics := sortedMetricKeys(bundle.Scores)
	normalized := make([]float64, 0, len(metrics))
	for _, metric := range metrics {
		score := bundle.Scores[metric]
		switch metric {
		case "overall", "overallScore":
			payload.OverallScore = score
		case "taskCompletionRate":
			runtimeMetrics.TaskCompletionRate = score
			runtimeSet = true
		case "blockingFailureRate":
			runtimeMetrics.BlockingFailureRate = score
			runtimeSet = true
		case "retrySuccessRate":
			runtimeMetrics.RetrySuccessRate = score
			runtimeSet = true
		case "tokenBudgetHitRate":
			runtimeMetrics.TokenBudgetHitRate = score
			runtimeSet = true
		case "endToEndLatencyMs", "latencyMs":
			runtimeMetrics.EndToEndLatencyMs = int64(score)
			runtimeSet = true
		case "toolCallCount":
			runtimeMetrics.ToolCallCount = int(score)
			runtimeSet = true
		case "answerCorrectness", "qaAccuracy":
			qualityMetrics.AnswerCorrectness = score
			qualitySet = true
		case "groundedness", "supportedClaimRatio", "abstentionAccuracy":
			qualityMetrics.Groundedness = score
			qualitySet = true
		case "citationPrecision":
			qualityMetrics.CitationPrecision = score
			qualitySet = true
		case "citationRecall":
			qualityMetrics.CitationRecall = score
			qualitySet = true
		case "toolPrecision":
			qualityMetrics.ToolPrecision = score
			qualitySet = true
		case "toolRecall":
			qualityMetrics.ToolRecall = score
			qualitySet = true
		case "toolArgAccuracy":
			qualityMetrics.ToolArgAccuracy = score
			qualitySet = true
		case "synthesisInputCoverage":
			qualityMetrics.SynthesisInputCoverage = score
			qualitySet = true
		}
		normalized = append(normalized, clampUnit(score))
	}

	if bundle.Cost.LatencyMs != 0 {
		runtimeMetrics.EndToEndLatencyMs = bundle.Cost.LatencyMs
		runtimeSet = true
	}

	if payload.OverallScore == 0 {
		payload.OverallScore = evaluationScoreAverage(normalized)
	}
	payload.Level = evaluation.ScoreLevelForOverallScoreWithReplayConsistency(payload.OverallScore, payload.ReplayConsistent)

	if runtimeSet {
		payload.RuntimeMetrics = runtimeMetrics
	}
	if qualitySet {
		payload.QualityMetrics = qualityMetrics
	}
	return payload
}

func sortedMetricKeys(scores map[string]float64) []string {
	keys := make([]string, 0, len(scores))
	for key := range scores {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func clampUnit(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func evaluationScoreAverage(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}
