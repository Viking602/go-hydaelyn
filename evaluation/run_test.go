package evaluation

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEvalRunSchema(t *testing.T) {
	t.Parallel()

	t.Run("round trip", func(t *testing.T) {
		t.Parallel()

		startedAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
		completedAt := time.Date(2026, time.January, 2, 3, 6, 7, 0, time.UTC)

		want := EvalRun{
			SchemaVersion:     EvalRunSchemaVersion,
			ID:                "run-123",
			CaseID:            "deepsearch-basic",
			Mode:              EvalRunModeDeterministic,
			RuntimeConfigHash: "sha256:abc123",
			Provenance: &EvalRunProvenance{
				LaneID:             "nightly-live",
				Provider:           "openai",
				ProviderVersion:    "v1",
				ProviderProvenance: "openai-public-api",
				Model:              "gpt-5.4",
				ModelProvenance:    "snapshot-2026-01-01",
				ResolvedBaseURL:    "https://api.openai.com/v1",
			},
			Usage: &EvalRunUsage{
				PromptTokens:     120,
				CompletionTokens: 45,
				TotalTokens:      165,
				TotalCostUSD:     0.0125,
				LatencyMs:        2000,
				ToolCallCount:    2,
			},
			Variance: &EvalRunVariance{
				ComparedRunID:   "run-122",
				Window:          1,
				MetricDeltas:    map[string]float64{"overallScore": -0.02, "groundedness": 0.01},
				ExceededMetrics: []string{"overallScore"},
			},
			Seed:        42,
			StartedAt:   startedAt,
			CompletedAt: completedAt,
			TraceRefs: &EvalRunTraceRefs{
				Events: &EvalRunRef{
					ID:   "trace-events",
					Path: "runs/run-123/traces/events.ndjson",
					URI:  "file://runs/run-123/traces/events.ndjson",
				},
				ModelEvents: &EvalRunRef{
					ID:   "trace-model-events",
					Path: "runs/run-123/traces/model-events.ndjson",
					URI:  "file://runs/run-123/traces/model-events.ndjson",
				},
			},
			ArtifactRefs: &EvalRunArtifactRefs{
				Events: &EvalRunRef{
					ID:   "artifact-events-json",
					Path: "runs/run-123/events.json",
					URI:  "file://runs/run-123/events.json",
				},
				Replay: &EvalRunRef{
					ID:   "artifact-replay-json",
					Path: "runs/run-123/replay.json",
					URI:  "file://runs/run-123/replay.json",
				},
				Answer: &EvalRunRef{
					ID:   "artifact-answer-txt",
					Path: "runs/run-123/answer.txt",
					URI:  "file://runs/run-123/answer.txt",
				},
			},
			ScoreRef: &EvalRunRef{
				ID:   "artifact-score-json",
				Path: "runs/run-123/score.json",
				URI:  "file://runs/run-123/score.json",
			},
			PolicyOutcomes: []EvalRunPolicyOutcome{
				{
					Policy:    "groundedness",
					Outcome:   "passed",
					Severity:  "info",
					Message:   "all required citations were present",
					Reference: "policy://groundedness",
				},
				{
					Policy:    "safety",
					Outcome:   "blocked",
					Severity:  "high",
					Message:   "unsafe answer variant was filtered",
					Blocking:  true,
					Reference: "policy://safety",
				},
			},
			Status: EvalRunStatusCompleted,
		}

		data, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("marshal eval run: %v", err)
		}

		jsonText := string(data)
		for _, fragment := range []string{
			`"schemaVersion":"1.0"`,
			`"mode":"deterministic"`,
			`"caseId":"deepsearch-basic"`,
			`"runtimeConfigHash":"sha256:abc123"`,
			`"provenance":{`,
			`"usage":{`,
			`"variance":{`,
			`"scoreRef":{`,
			`"policyOutcomes":[`,
			`"status":"completed"`,
		} {
			if !strings.Contains(jsonText, fragment) {
				t.Fatalf("expected marshaled JSON to contain %q, got %s", fragment, jsonText)
			}
		}

		var got EvalRun
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal eval run: %v", err)
		}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round-trip mismatch\nwant: %#v\ngot:  %#v", want, got)
		}
	})

	t.Run("omit optional sections", func(t *testing.T) {
		t.Parallel()

		run := EvalRun{
			SchemaVersion: EvalRunSchemaVersion,
			ID:            "run-minimal",
			CaseID:        "case-minimal",
			Mode:          EvalRunModeLive,
			StartedAt:     time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC),
			CompletedAt:   time.Date(2026, time.February, 3, 4, 5, 7, 0, time.UTC),
			Status:        EvalRunStatusRunning,
		}

		data, err := json.Marshal(run)
		if err != nil {
			t.Fatalf("marshal minimal eval run: %v", err)
		}

		jsonText := string(data)
		for _, fragment := range []string{"provenance", "usage", "variance", "traceRefs", "artifactRefs", "scoreRef", "policyOutcomes", "error"} {
			if strings.Contains(jsonText, `"`+fragment+`"`) {
				t.Fatalf("expected marshaled JSON to omit %q, got %s", fragment, jsonText)
			}
		}
	})
}
