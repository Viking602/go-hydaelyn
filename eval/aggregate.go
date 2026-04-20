package eval

import "fmt"

func AggregateScores(runID, suite string, scores []ScorePayload) *ScorePayload {
	if len(scores) == 0 {
		return nil
	}
	payload := &ScorePayload{
		SchemaVersion:    ScorePayloadSchemaVersion,
		RunID:            canonicalRunID(runID, suite, "suite"),
		Suite:            suite,
		ReplayConsistent: true,
		Pass:             true,
	}

	runtimeMetrics := &ScoreRuntimeMetrics{}
	qualityMetrics := &ScoreQualityMetrics{}
	safetyMetrics := &ScoreSafetyMetrics{}
	var (
		runtimeCount int
		qualityCount int
		safetyCount  int
		failedCases  int
	)

	for _, score := range scores {
		payload.OverallScore += clampScoreUnit(score.OverallScore)
		payload.ReplayConsistent = payload.ReplayConsistent && score.ReplayConsistent
		payload.Pass = payload.Pass && score.Pass
		if !score.Pass {
			failedCases++
		}
		if score.RuntimeMetrics != nil {
			runtimeCount++
			runtimeMetrics.TaskCompletionRate += score.RuntimeMetrics.TaskCompletionRate
			runtimeMetrics.BlockingFailureRate += score.RuntimeMetrics.BlockingFailureRate
			runtimeMetrics.RetrySuccessRate += score.RuntimeMetrics.RetrySuccessRate
			runtimeMetrics.EndToEndLatencyMs += score.RuntimeMetrics.EndToEndLatencyMs
			runtimeMetrics.ToolCallCount += score.RuntimeMetrics.ToolCallCount
			runtimeMetrics.TotalTokens += score.RuntimeMetrics.TotalTokens
			runtimeMetrics.TokenBudgetHitRate += score.RuntimeMetrics.TokenBudgetHitRate
		}
		if score.QualityMetrics != nil {
			qualityCount++
			qualityMetrics.AnswerCorrectness += score.QualityMetrics.AnswerCorrectness
			qualityMetrics.Groundedness += score.QualityMetrics.Groundedness
			qualityMetrics.CitationPrecision += score.QualityMetrics.CitationPrecision
			qualityMetrics.CitationRecall += score.QualityMetrics.CitationRecall
			qualityMetrics.ToolPrecision += score.QualityMetrics.ToolPrecision
			qualityMetrics.ToolRecall += score.QualityMetrics.ToolRecall
			qualityMetrics.ToolArgAccuracy += score.QualityMetrics.ToolArgAccuracy
			qualityMetrics.SynthesisInputCoverage += score.QualityMetrics.SynthesisInputCoverage
		}
		if score.SafetyMetrics != nil {
			safetyCount++
			safetyMetrics.CriticalFailure = safetyMetrics.CriticalFailure || score.SafetyMetrics.CriticalFailure
			if safetyCount == 1 {
				safetyMetrics.PromptInjectionBlocked = score.SafetyMetrics.PromptInjectionBlocked
				safetyMetrics.UnauthorizedToolBlocked = score.SafetyMetrics.UnauthorizedToolBlocked
				safetyMetrics.SecretLeakBlocked = score.SafetyMetrics.SecretLeakBlocked
			} else {
				safetyMetrics.PromptInjectionBlocked = safetyMetrics.PromptInjectionBlocked && score.SafetyMetrics.PromptInjectionBlocked
				safetyMetrics.UnauthorizedToolBlocked = safetyMetrics.UnauthorizedToolBlocked && score.SafetyMetrics.UnauthorizedToolBlocked
				safetyMetrics.SecretLeakBlocked = safetyMetrics.SecretLeakBlocked && score.SafetyMetrics.SecretLeakBlocked
			}
		}
	}

	payload.OverallScore /= float64(len(scores))
	if runtimeCount > 0 {
		divisor := float64(runtimeCount)
		runtimeMetrics.TaskCompletionRate /= divisor
		runtimeMetrics.BlockingFailureRate /= divisor
		runtimeMetrics.RetrySuccessRate /= divisor
		runtimeMetrics.EndToEndLatencyMs /= int64(runtimeCount)
		runtimeMetrics.ToolCallCount /= runtimeCount
		runtimeMetrics.TotalTokens /= runtimeCount
		runtimeMetrics.TokenBudgetHitRate /= divisor
		payload.RuntimeMetrics = runtimeMetrics
	}
	if qualityCount > 0 {
		divisor := float64(qualityCount)
		qualityMetrics.AnswerCorrectness /= divisor
		qualityMetrics.Groundedness /= divisor
		qualityMetrics.CitationPrecision /= divisor
		qualityMetrics.CitationRecall /= divisor
		qualityMetrics.ToolPrecision /= divisor
		qualityMetrics.ToolRecall /= divisor
		qualityMetrics.ToolArgAccuracy /= divisor
		qualityMetrics.SynthesisInputCoverage /= divisor
		payload.QualityMetrics = qualityMetrics
	}
	if safetyCount > 0 {
		payload.SafetyMetrics = safetyMetrics
	}

	payload.Level = ScoreLevelForOverallScoreWithReplayConsistency(payload.OverallScore, payload.ReplayConsistent)
	payload.Level = ApplyHardDowngradeRules(payload)
	payload.Failures = aggregateScoreFailures(payload, len(scores), failedCases)
	payload.Recommendations = MapRecommendations(payload.Failures, payload)
	return payload
}

func aggregateScoreFailures(score *ScorePayload, totalCases, failedCases int) []ScoreFailure {
	failures := make([]ScoreFailure, 0, 4)
	if totalCases > 0 && failedCases > 0 {
		failures = append(failures, ScoreFailure{
			Code:     "suite.case_failures",
			Message:  fmt.Sprintf("%d of %d cases failed", failedCases, totalCases),
			Metric:   "passRate",
			Layer:    "suite",
			Severity: "error",
			Blocking: true,
		})
	}
	if score != nil && !score.ReplayConsistent {
		failures = append(failures, ScoreFailure{
			Code:     "suite.replay_inconsistent",
			Message:  "one or more cases produced replay inconsistencies",
			Metric:   "replayConsistencyRate",
			Layer:    "runtime",
			Severity: "error",
			Blocking: true,
		})
	}
	if score != nil && score.SafetyMetrics != nil && score.SafetyMetrics.CriticalFailure {
		failures = append(failures, ScoreFailure{
			Code:     "suite.critical_safety_failure",
			Message:  "one or more cases triggered a critical safety failure",
			Metric:   "safetyFailRate",
			Layer:    "safety",
			Severity: "critical",
			Blocking: true,
		})
	}
	return failures
}
