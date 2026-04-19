package evaluation

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Viking602/go-hydaelyn/storage"
)

func ExtractFailures(score *ScorePayload, events []Event, caseDef EvalCase) []ScoreFailure {
	if score == nil {
		return nil
	}
	failures := make([]ScoreFailure, 0, 8)

	if score.SafetyMetrics != nil && score.SafetyMetrics.CriticalFailure {
		failures = append(failures, ScoreFailure{
			Code:     "critical-safety-failure",
			Message:  "critical safety policy produced a blocking failure",
			Metric:   "criticalFailure",
			Layer:    classifyRootCauseLayer("criticalFailure", "safety critical policy"),
			Severity: "critical",
			Blocking: true,
		})
	}
	if !score.ReplayConsistent {
		failures = append(failures, ScoreFailure{
			Code:     "replay-inconsistent",
			Message:  "replay validation reported an inconsistent run",
			Metric:   "replayConsistent",
			Layer:    classifyRootCauseLayer("replayConsistent", "replay validation"),
			Severity: "high",
			Blocking: true,
		})
	}
	if score.RuntimeMetrics != nil {
		if score.RuntimeMetrics.TaskCompletionRate < 0.80 {
			failures = append(failures, metricFailure("low-task-completion", "taskCompletionRate", score.RuntimeMetrics.TaskCompletionRate, 0.80, true))
		}
		if caseDef.Thresholds != nil && caseDef.Thresholds.TaskCompletionRate > 0 && score.RuntimeMetrics.TaskCompletionRate < caseDef.Thresholds.TaskCompletionRate {
			failures = append(failures, thresholdFailure("task-completion-threshold-miss", "taskCompletionRate", score.RuntimeMetrics.TaskCompletionRate, caseDef.Thresholds.TaskCompletionRate))
		}
		if score.RuntimeMetrics.BlockingFailureRate > 0 {
			failures = append(failures, ScoreFailure{
				Code:     "blocking-failures",
				Message:  fmt.Sprintf("blocking failure rate %.2f exceeded zero", score.RuntimeMetrics.BlockingFailureRate),
				Metric:   "blockingFailureRate",
				Layer:    classifyRootCauseLayer("blockingFailureRate", "blocking task failures"),
				Severity: failureSeverityForGap(score.RuntimeMetrics.BlockingFailureRate),
				Blocking: score.RuntimeMetrics.BlockingFailureRate >= 0.20,
			})
		}
		if threshold := caseDefRetryThreshold(caseDef); threshold > 0 && score.RuntimeMetrics.RetrySuccessRate < threshold {
			failures = append(failures, thresholdFailure("retry-success-threshold-miss", "retrySuccessRate", score.RuntimeMetrics.RetrySuccessRate, threshold))
		}
		if caseDef.Limits != nil && caseDef.Limits.MaxToolCalls > 0 && score.RuntimeMetrics.ToolCallCount > caseDef.Limits.MaxToolCalls {
			failures = append(failures, ScoreFailure{
				Code:     "tool-call-limit-exceeded",
				Message:  fmt.Sprintf("toolCallCount %d exceeded case limit %d", score.RuntimeMetrics.ToolCallCount, caseDef.Limits.MaxToolCalls),
				Metric:   "toolCallCount",
				Layer:    classifyRootCauseLayer("toolCallCount", "tool call budget exceeded"),
				Severity: "medium",
				Blocking: true,
			})
		}
		if caseDef.Limits != nil && caseDef.Limits.MaxLatencyMs > 0 && score.RuntimeMetrics.EndToEndLatencyMs > int64(caseDef.Limits.MaxLatencyMs) {
			failures = append(failures, ScoreFailure{
				Code:     "latency-limit-exceeded",
				Message:  fmt.Sprintf("endToEndLatencyMs %d exceeded case limit %d", score.RuntimeMetrics.EndToEndLatencyMs, caseDef.Limits.MaxLatencyMs),
				Metric:   "endToEndLatencyMs",
				Layer:    classifyRootCauseLayer("endToEndLatencyMs", "latency budget exceeded"),
				Severity: "medium",
				Blocking: true,
			})
		}
		if caseDef.Limits != nil && caseDef.Limits.MaxTokens > 0 && score.RuntimeMetrics.TotalTokens > caseDef.Limits.MaxTokens {
			failures = append(failures, ScoreFailure{
				Code:     "token-limit-exceeded",
				Message:  fmt.Sprintf("totalTokens %d exceeded case limit %d", score.RuntimeMetrics.TotalTokens, caseDef.Limits.MaxTokens),
				Metric:   "totalTokens",
				Layer:    classifyRootCauseLayer("totalTokens", "token budget exceeded"),
				Severity: "medium",
				Blocking: true,
			})
		}
	}
	if score.QualityMetrics != nil {
		if score.QualityMetrics.Groundedness < 0.70 {
			failures = append(failures, metricFailure("low-groundedness", "groundedness", score.QualityMetrics.Groundedness, 0.70, true))
		}
		if score.QualityMetrics.SynthesisInputCoverage < 0.80 {
			failures = append(failures, metricFailure("low-synthesis-coverage", "synthesisInputCoverage", score.QualityMetrics.SynthesisInputCoverage, 0.80, true))
		}
		if threshold := caseDefGroundednessThreshold(caseDef); threshold > 0 && score.QualityMetrics.Groundedness < threshold {
			failures = append(failures, thresholdFailure("groundedness-threshold-miss", "groundedness", score.QualityMetrics.Groundedness, threshold))
		}
		if threshold := caseDefAnswerCorrectnessThreshold(caseDef); threshold > 0 && score.QualityMetrics.AnswerCorrectness < threshold {
			failures = append(failures, thresholdFailure("answer-correctness-threshold-miss", "answerCorrectness", score.QualityMetrics.AnswerCorrectness, threshold))
		}
		if score.QualityMetrics.CitationPrecision > 0 && score.QualityMetrics.CitationPrecision < 0.80 {
			failures = append(failures, metricFailure("low-citation-precision", "citationPrecision", score.QualityMetrics.CitationPrecision, 0.80, false))
		}
		if threshold := caseDefCitationPrecisionThreshold(caseDef); threshold > 0 && score.QualityMetrics.CitationPrecision < threshold {
			failures = append(failures, thresholdFailure("citation-precision-threshold-miss", "citationPrecision", score.QualityMetrics.CitationPrecision, threshold))
		}
		if score.QualityMetrics.CitationRecall > 0 && score.QualityMetrics.CitationRecall < 0.80 {
			failures = append(failures, metricFailure("low-citation-recall", "citationRecall", score.QualityMetrics.CitationRecall, 0.80, false))
		}
		if threshold := caseDefCitationRecallThreshold(caseDef); threshold > 0 && score.QualityMetrics.CitationRecall < threshold {
			failures = append(failures, thresholdFailure("citation-recall-threshold-miss", "citationRecall", score.QualityMetrics.CitationRecall, threshold))
		}
		if score.QualityMetrics.ToolPrecision > 0 && score.QualityMetrics.ToolPrecision < 0.75 {
			failures = append(failures, metricFailure("low-tool-precision", "toolPrecision", score.QualityMetrics.ToolPrecision, 0.75, false))
		}
		if threshold := caseDefToolPrecisionThreshold(caseDef); threshold > 0 && score.QualityMetrics.ToolPrecision < threshold {
			failures = append(failures, thresholdFailure("tool-precision-threshold-miss", "toolPrecision", score.QualityMetrics.ToolPrecision, threshold))
		}
		if score.QualityMetrics.ToolRecall > 0 && score.QualityMetrics.ToolRecall < 0.75 {
			failures = append(failures, metricFailure("low-tool-recall", "toolRecall", score.QualityMetrics.ToolRecall, 0.75, false))
		}
		if threshold := caseDefToolRecallThreshold(caseDef); threshold > 0 && score.QualityMetrics.ToolRecall < threshold {
			failures = append(failures, thresholdFailure("tool-recall-threshold-miss", "toolRecall", score.QualityMetrics.ToolRecall, threshold))
		}
		if score.QualityMetrics.ToolArgAccuracy > 0 && score.QualityMetrics.ToolArgAccuracy < 0.80 {
			failures = append(failures, metricFailure("low-tool-arg-accuracy", "toolArgAccuracy", score.QualityMetrics.ToolArgAccuracy, 0.80, false))
		}
		if threshold := caseDefToolArgAccuracyThreshold(caseDef); threshold > 0 && score.QualityMetrics.ToolArgAccuracy < threshold {
			failures = append(failures, thresholdFailure("tool-arg-accuracy-threshold-miss", "toolArgAccuracy", score.QualityMetrics.ToolArgAccuracy, threshold))
		}
		if threshold := caseDefSynthesisCoverageThreshold(caseDef); threshold > 0 && score.QualityMetrics.SynthesisInputCoverage < threshold {
			failures = append(failures, thresholdFailure("synthesis-coverage-threshold-miss", "synthesisInputCoverage", score.QualityMetrics.SynthesisInputCoverage, threshold))
		}
	}

	failures = append(failures, failuresFromEvents(events)...)
	return topFailures(failures, 5)
}

func failuresFromEvents(events []Event) []ScoreFailure {
	failures := make([]ScoreFailure, 0)
	for _, event := range events {
		switch event.Type {
		case storage.EventTaskFailed:
			message := strings.TrimSpace(payloadStringValue(event.Payload["error"]))
			if message == "" {
				message = fmt.Sprintf("task %s failed", strings.TrimSpace(event.TaskID))
			}
			failures = append(failures, ScoreFailure{
				Code:     "task-failed",
				Message:  message,
				Metric:   "taskCompletionRate",
				Layer:    classifyRootCauseLayer("taskCompletionRate", message),
				Severity: "high",
				Blocking: payloadBoolValue(event.Payload["blocking"]),
			})
		case storage.EventVerifierBlocked, storage.EventPolicyOutcome:
			for _, outcome := range extractPolicyOutcomes([]storage.Event{event}) {
				if !outcome.Blocking && !strings.EqualFold(outcome.Outcome, "blocked") && !strings.EqualFold(outcome.Outcome, "denied") {
					continue
				}
				severity := normalizeFailureSeverity(outcome.Severity)
				if severity == "" {
					severity = "high"
				}
				message := strings.TrimSpace(outcome.Message)
				if message == "" {
					message = fmt.Sprintf("policy %s returned %s", outcome.Policy, outcome.Outcome)
				}
				failures = append(failures, ScoreFailure{
					Code:     "policy-blocked",
					Message:  message,
					Metric:   outcome.Policy,
					Layer:    classifyRootCauseLayer(outcome.Policy, message),
					Severity: severity,
					Blocking: true,
				})
			}
		}
	}
	return failures
}

func metricFailure(code, metric string, actual, threshold float64, blocking bool) ScoreFailure {
	message := fmt.Sprintf("%s %.2f fell below %.2f", metric, actual, threshold)
	return ScoreFailure{
		Code:     code,
		Message:  message,
		Metric:   metric,
		Layer:    classifyRootCauseLayer(metric, message),
		Severity: failureSeverityForGap(threshold - actual),
		Blocking: blocking,
	}
}

func thresholdFailure(code, metric string, actual, threshold float64) ScoreFailure {
	failure := metricFailure(code, metric, actual, threshold, false)
	failure.Message = fmt.Sprintf("%s %.2f missed case threshold %.2f", metric, actual, threshold)
	return failure
}

func failureSeverityForGap(gap float64) string {
	if gap >= 0.25 {
		return "critical"
	}
	if gap >= 0.15 {
		return "high"
	}
	if gap >= 0.05 {
		return "medium"
	}
	return "low"
}

func normalizeFailureSeverity(severity string) string {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		return "critical"
	case "high", "error":
		return "high"
	case "medium", "warning", "warn":
		return "medium"
	case "low", "info":
		return "low"
	default:
		return ""
	}
}

func topFailures(failures []ScoreFailure, limit int) []ScoreFailure {
	if len(failures) == 0 {
		return nil
	}
	unique := make([]ScoreFailure, 0, len(failures))
	seen := map[string]struct{}{}
	for _, failure := range failures {
		key := failure.Code + "|" + failure.Metric + "|" + failure.Message
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, failure)
	}
	sort.SliceStable(unique, func(i, j int) bool {
		if failureSeverityRank(unique[i].Severity) != failureSeverityRank(unique[j].Severity) {
			return failureSeverityRank(unique[i].Severity) > failureSeverityRank(unique[j].Severity)
		}
		if unique[i].Blocking != unique[j].Blocking {
			return unique[i].Blocking
		}
		if unique[i].Metric != unique[j].Metric {
			return unique[i].Metric < unique[j].Metric
		}
		return unique[i].Code < unique[j].Code
	})
	if limit > 0 && len(unique) > limit {
		unique = unique[:limit]
	}
	return unique
}

func failureSeverityRank(severity string) int {
	switch normalizeFailureSeverity(severity) {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	default:
		return 1
	}
}

func classifyRootCauseLayer(metric, detail string) string {
	text := strings.ToLower(strings.TrimSpace(metric + " " + detail))
	switch {
	case strings.Contains(text, "tool"), strings.Contains(text, "permission"), strings.Contains(text, "unauthorized"):
		return "tool"
	case strings.Contains(text, "planner"), strings.Contains(text, "synthesis"), strings.Contains(text, "taskcompletion"):
		return "planner"
	case strings.Contains(text, "prompt"), strings.Contains(text, "injection"), strings.Contains(text, "grounded"), strings.Contains(text, "citation"), strings.Contains(text, "verifier"):
		return "prompt"
	case strings.Contains(text, "answercorrectness"), strings.Contains(text, "model"), strings.Contains(text, "hallucin"):
		return "model"
	case strings.Contains(text, "runtime"), strings.Contains(text, "replay"), strings.Contains(text, "latency"), strings.Contains(text, "token"):
		return "runtime"
	default:
		return "runtime"
	}
}

func caseDefGroundednessThreshold(caseDef EvalCase) float64 {
	if caseDef.Thresholds == nil {
		return 0
	}
	if caseDef.Thresholds.Groundedness > 0 {
		return caseDef.Thresholds.Groundedness
	}
	return caseDef.Thresholds.SupportedClaimRatio
}

func caseDefRetryThreshold(caseDef EvalCase) float64 {
	if caseDef.Thresholds == nil {
		return 0
	}
	return caseDef.Thresholds.RetrySuccessRate
}

func caseDefAnswerCorrectnessThreshold(caseDef EvalCase) float64 {
	if caseDef.Thresholds == nil {
		return 0
	}
	return caseDef.Thresholds.AnswerCorrectness
}

func caseDefCitationPrecisionThreshold(caseDef EvalCase) float64 {
	if caseDef.Thresholds == nil {
		return 0
	}
	return caseDef.Thresholds.CitationPrecision
}

func caseDefCitationRecallThreshold(caseDef EvalCase) float64 {
	if caseDef.Thresholds == nil {
		return 0
	}
	return caseDef.Thresholds.CitationRecall
}

func caseDefToolPrecisionThreshold(caseDef EvalCase) float64 {
	if caseDef.Thresholds == nil {
		return 0
	}
	return caseDef.Thresholds.ToolPrecision
}

func caseDefToolRecallThreshold(caseDef EvalCase) float64 {
	if caseDef.Thresholds == nil {
		return 0
	}
	return caseDef.Thresholds.ToolRecall
}

func caseDefToolArgAccuracyThreshold(caseDef EvalCase) float64 {
	if caseDef.Thresholds == nil {
		return 0
	}
	return caseDef.Thresholds.ToolArgAccuracy
}

func caseDefSynthesisCoverageThreshold(caseDef EvalCase) float64 {
	if caseDef.Thresholds == nil {
		return 0
	}
	return caseDef.Thresholds.SynthesisInputCoverage
}
