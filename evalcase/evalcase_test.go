package evalcase

import (
	"path/filepath"
	"testing"

	"github.com/Viking602/go-hydaelyn/evaluation"
)

func TestValidateCase(t *testing.T) {
	t.Run("valid case", func(t *testing.T) {
		err := ValidateCase(evaluation.EvalCase{
			SchemaVersion: evaluation.EvalCaseSchemaVersion,
			ID:            "case-1",
			Suite:         "fixture",
			Pattern:       "deepsearch",
			Profiles:      &evaluation.EvalCaseProfiles{Supervisor: "supervisor", Worker: "worker"},
			Tools:         []string{"fixture_search", "calculator"},
			Fixtures:      &evaluation.EvalCaseFixtures{CorpusIDs: []string{"policy-001"}, Paths: []string{"fixtures/corpus"}},
			Thresholds:    &evaluation.EvalCaseThresholds{Groundedness: 0.8, RetrySuccessRate: 1},
			Limits:        &evaluation.EvalCaseLimits{MaxToolCalls: 3, MaxLatencyMs: 1000, MaxTokens: 500},
		})
		if err != nil {
			t.Fatalf("ValidateCase() error = %v", err)
		}
	})

	t.Run("invalid threshold", func(t *testing.T) {
		err := ValidateCase(evaluation.EvalCase{
			SchemaVersion: evaluation.EvalCaseSchemaVersion,
			ID:            "case-1",
			Suite:         "fixture",
			Pattern:       "deepsearch",
			Thresholds:    &evaluation.EvalCaseThresholds{Groundedness: 1.2},
		})
		if err == nil {
			t.Fatal("expected validation error")
		}
	})
}

func TestLoadCase(t *testing.T) {
	path := filepath.Join("testdata", "case.json")
	c, err := LoadCase(path)
	if err != nil {
		t.Fatalf("LoadCase() error = %v", err)
	}
	if c.ID != "case-loader" {
		t.Fatalf("ID = %q, want case-loader", c.ID)
	}
	if c.Expected == nil || len(c.Expected.RequiredCitations) != 1 {
		t.Fatalf("expected required citations, got %#v", c.Expected)
	}
}

func TestLoadCorpus(t *testing.T) {
	path := filepath.Join("..", "fixtures", "corpus")
	docs, err := LoadCorpus(path)
	if err != nil {
		t.Fatalf("LoadCorpus() error = %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("len(docs) = %d, want 2", len(docs))
	}
	if _, ok := docs["policy-001"]; !ok {
		t.Fatalf("expected policy-001 document")
	}
	if _, ok := docs["evidence-001"]; !ok {
		t.Fatalf("expected evidence-001 document")
	}
}
