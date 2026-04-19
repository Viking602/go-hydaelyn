package evaluation

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestEvalCaseSchema(t *testing.T) {
	t.Parallel()

	want := EvalCase{
		SchemaVersion: EvalCaseSchemaVersion,
		ID:            "deepsearch-basic",
		Suite:         "deepsearch",
		Pattern:       "deepsearch",
		Input: map[string]any{
			"query":      "compare Go agent runtimes",
			"subqueries": []any{"runtime design", "evaluation contracts"},
			"options": map[string]any{
				"requireVerification": true,
			},
		},
		Profiles: &EvalCaseProfiles{
			Supervisor: "supervisor",
			Worker:     "researcher",
			Workers:    []string{"researcher-2", "researcher-3"},
		},
		Tools: []string{"search", "calculator"},
		Fixtures: &EvalCaseFixtures{
			CorpusIDs: []string{"corpus-deepsearch"},
			Paths:     []string{"fixtures/deepsearch/basic"},
		},
		Expected: &EvalCaseExpected{
			MustInclude:       []string{"runtime design", "evaluation contracts"},
			MustNotInclude:    []string{"hallucinated citation"},
			RequiredCitations: []string{"corpus-deepsearch/doc-1"},
		},
		Thresholds: &EvalCaseThresholds{
			TaskCompletionRate:     1,
			AnswerCorrectness:      0.95,
			Groundedness:           0.9,
			SupportedClaimRatio:    0.8,
			CitationPrecision:      0.9,
			CitationRecall:         0.85,
			ToolPrecision:          0.9,
			ToolRecall:             0.85,
			ToolArgAccuracy:        0.8,
			SynthesisInputCoverage: 0.95,
			RetrySuccessRate:       0.5,
		},
		Limits: &EvalCaseLimits{
			MaxToolCalls: 8,
			MaxLatencyMs: 5000,
			MaxTokens:    12000,
		},
	}

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal eval case: %v", err)
	}

	jsonText := string(data)
	for _, fragment := range []string{"\"schemaVersion\":\"1.0\"", "\"taskCompletionRate\":1", "\"maxToolCalls\":8"} {
		if !strings.Contains(jsonText, fragment) {
			t.Fatalf("expected marshaled JSON to contain %q, got %s", fragment, jsonText)
		}
	}

	var got EvalCase
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal eval case: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-trip mismatch\nwant: %#v\ngot:  %#v", want, got)
	}
}
