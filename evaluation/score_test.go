package evaluation

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestScorePayloadSchema(t *testing.T) {
	t.Parallel()

	t.Run("round trip", func(t *testing.T) {
		t.Parallel()

		want := ScorePayload{
			SchemaVersion: ScorePayloadSchemaVersion,
			RunID:         "run-123",
			OverallScore:  0.91,
			Level:         ScoreLevelA4,
			RuntimeMetrics: &ScoreRuntimeMetrics{
				TaskCompletionRate:  1,
				BlockingFailureRate: 0,
				RetrySuccessRate:    0.5,
				EndToEndLatencyMs:   4200,
				ToolCallCount:       6,
				TokenBudgetHitRate:  0.25,
			},
			QualityMetrics: &ScoreQualityMetrics{
				AnswerCorrectness:      0.92,
				Groundedness:           0.95,
				CitationPrecision:      0.9,
				CitationRecall:         0.88,
				ToolPrecision:          0.85,
				ToolRecall:             0.83,
				ToolArgAccuracy:        0.9,
				SynthesisInputCoverage: 1,
			},
			SafetyMetrics: &ScoreSafetyMetrics{
				PromptInjectionBlocked:  true,
				UnauthorizedToolBlocked: true,
				SecretLeakBlocked:       true,
			},
			Failures: []ScoreFailure{
				{
					Code:     "retry-exhausted",
					Message:  "one branch needed a second attempt",
					Metric:   "retrySuccessRate",
					Severity: "medium",
				},
			},
			Recommendations: []ScoreRecommendation{
				{
					Priority:  "high",
					Action:    "tighten tool argument validation",
					Rationale: "tool arg accuracy trails groundedness",
				},
			},
		}

		data, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("marshal score payload: %v", err)
		}

		jsonText := string(data)
		for _, fragment := range []string{
			`"schemaVersion":"1.0"`,
			`"level":"A4"`,
			`"runtimeMetrics":{`,
			`"answerCorrectness":0.92`,
			`"secretLeakBlocked":true`,
			`"recommendations":[`,
		} {
			if !strings.Contains(jsonText, fragment) {
				t.Fatalf("expected marshaled JSON to contain %q, got %s", fragment, jsonText)
			}
		}

		var got ScorePayload
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal score payload: %v", err)
		}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round-trip mismatch\nwant: %#v\ngot:  %#v", want, got)
		}
	})

	t.Run("omit optional sections", func(t *testing.T) {
		t.Parallel()

		payload := ScorePayload{
			SchemaVersion: ScorePayloadSchemaVersion,
			RunID:         "run-minimal",
		}

		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal minimal score payload: %v", err)
		}

		jsonText := string(data)
		for _, fragment := range []string{"runtimeMetrics", "qualityMetrics", "safetyMetrics", "failures", "recommendations", "level"} {
			if strings.Contains(jsonText, `"`+fragment+`"`) {
				t.Fatalf("expected marshaled JSON to omit %q, got %s", fragment, jsonText)
			}
		}
	})
}
