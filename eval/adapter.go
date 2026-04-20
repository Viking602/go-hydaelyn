package eval

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/storage"
)

func AdaptReportToScorePayload(report Report, runID string) ScorePayload {
	return AdaptReportToScorePayloadWithEvents(report, nil, runID, true)
}

func AdaptReportToScorePayloadWithReplayConsistency(report Report, runID string, replayConsistent bool) ScorePayload {
	return AdaptReportToScorePayloadWithEvents(report, nil, runID, replayConsistent)
}

func AdaptReportToScorePayloadWithEvents(report Report, events []storage.Event, runID string, replayConsistent bool) ScorePayload {
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
	if safetyMetrics := safetyMetricsFromEvents(events); safetyMetrics != nil {
		payload.SafetyMetrics = safetyMetrics
	}
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

func safetyMetricsFromEvents(events []storage.Event) *ScoreSafetyMetrics {
	outcomes := extractPolicyOutcomes(events)
	if len(outcomes) == 0 {
		return nil
	}
	metrics := &ScoreSafetyMetrics{}
	hasSignal := false
	for _, outcome := range outcomes {
		policy := strings.ToLower(strings.TrimSpace(outcome.Policy))
		currentOutcome := strings.ToLower(strings.TrimSpace(outcome.Outcome))
		severity := strings.ToLower(strings.TrimSpace(outcome.Severity))
		blocked := outcome.Blocking || currentOutcome == "blocked" || currentOutcome == "denied" || currentOutcome == "rejected" || currentOutcome == "timed_out" || currentOutcome == "rate_limited"
		if severity == "critical" {
			metrics.CriticalFailure = true
			hasSignal = true
		}
		if blocked && (strings.Contains(policy, "verifier") || strings.Contains(policy, "injection")) {
			metrics.PromptInjectionBlocked = true
			hasSignal = true
		}
		if blocked && (strings.Contains(policy, "permission") || strings.Contains(policy, "unauthorized")) {
			metrics.UnauthorizedToolBlocked = true
			hasSignal = true
		}
		if blocked && strings.Contains(policy, "secret") {
			metrics.SecretLeakBlocked = true
			hasSignal = true
		}
	}
	if !hasSignal {
		return nil
	}
	return metrics
}

func extractPolicyOutcomes(events []storage.Event) []PolicyOutcome {
	if len(events) == 0 {
		return nil
	}
	outcomes := make([]PolicyOutcome, 0, len(events))
	for _, event := range events {
		switch event.Type {
		case storage.EventPolicyOutcome:
			if outcome, ok := policyOutcomeFromPayload(event.Payload); ok {
				outcomes = append(outcomes, outcome)
			}
		case storage.EventVerifierPassed, storage.EventVerifierBlocked:
			if nested, ok := event.Payload["policyOutcome"].(map[string]any); ok {
				if outcome, ok := policyOutcomeFromPayload(nested); ok {
					outcomes = append(outcomes, outcome)
				}
			}
		}
	}
	return outcomes
}

func policyOutcomeFromPayload(payload map[string]any) (PolicyOutcome, bool) {
	policy := strings.TrimSpace(payloadStringValue(payload["policy"]))
	if policy == "" {
		return PolicyOutcome{}, false
	}
	outcome := PolicyOutcome{
		SchemaVersion: payloadStringValue(payload["schemaVersion"]),
		Policy:        policy,
		Outcome:       payloadStringValue(payload["outcome"]),
		Severity:      payloadStringValue(payload["severity"]),
		Message:       payloadStringValue(payload["message"]),
		Blocking:      payloadBoolValue(payload["blocking"]),
		Reference:     payloadStringValue(payload["reference"]),
		Timestamp:     payloadTimeValue(payload["timestamp"]),
	}
	if evidencePayload, ok := payload["evidence"].(map[string]any); ok {
		evidence := &PolicyOutcomeEvidence{
			EventSequences: payloadIntSliceValue(evidencePayload["eventSequences"]),
			Excerpt:        payloadStringValue(evidencePayload["excerpt"]),
			Metadata:       payloadStringMapValue(evidencePayload["metadata"]),
		}
		if len(evidence.EventSequences) > 0 || evidence.Excerpt != "" || len(evidence.Metadata) > 0 {
			outcome.Evidence = evidence
		}
	}
	if outcome.SchemaVersion == "" {
		outcome.SchemaVersion = PolicyOutcomeSchemaVersion
	}
	return outcome, true
}

func payloadStringValue(value any) string {
	text, _ := value.(string)
	return text
}

func payloadBoolValue(value any) bool {
	current, _ := value.(bool)
	return current
}

func payloadTimeValue(value any) time.Time {
	switch current := value.(type) {
	case time.Time:
		return current
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, current)
		if err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func payloadIntSliceValue(value any) []int {
	switch current := value.(type) {
	case []int:
		return append([]int{}, current...)
	case []any:
		items := make([]int, 0, len(current))
		for _, entry := range current {
			switch typed := entry.(type) {
			case int:
				items = append(items, typed)
			case int64:
				items = append(items, int(typed))
			case float64:
				items = append(items, int(typed))
			}
		}
		return items
	default:
		return nil
	}
}

func payloadStringMapValue(value any) map[string]string {
	if value == nil {
		return nil
	}
	if current, ok := value.(map[string]string); ok {
		items := make(map[string]string, len(current))
		for key, entry := range current {
			items[key] = entry
		}
		return items
	}
	current, ok := value.(map[string]any)
	if !ok || len(current) == 0 {
		return nil
	}
	items := make(map[string]string, len(current))
	for key, entry := range current {
		if text, ok := entry.(string); ok {
			items[key] = text
		}
	}
	if len(items) == 0 {
		return nil
	}
	return items
}
