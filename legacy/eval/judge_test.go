package eval

import (
	"testing"
	"time"
)

func TestJudgePolicyDoesNotOverrideDeterministicFailure(t *testing.T) {
	t.Parallel()

	corpus := Corpus{Documents: []CorpusDocument{{
		ID:   "policy-001",
		Date: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		Text: "Policy 001: retain customer data for 30 days after account closure unless a legal hold is active.",
	}}}

	result := JudgeWithLLM("Customer data is retained for 60 days after account closure [policy-001].", corpus, JudgeConfig{
		Enabled:               true,
		MinGroundedness:       0.8,
		RequireValidCitations: true,
		Judge: func(answer string, corpus Corpus) (JudgeVerdict, error) {
			return JudgeVerdict{Pass: true, Score: 1, Summary: "looks fine", ModelName: "test-judge"}, nil
		},
	})

	if !result.Invoked || !result.ManifestRecorded {
		t.Fatalf("expected judge invocation to be recorded, got %+v", result)
	}
	if result.Passed {
		t.Fatalf("expected deterministic failure to hold, got %+v", result)
	}
	if result.DeterministicPass {
		t.Fatalf("expected deterministic failure, got %+v", result)
	}
	if len(result.DeterministicFailures) == 0 {
		t.Fatalf("expected deterministic failures, got %+v", result)
	}
}
