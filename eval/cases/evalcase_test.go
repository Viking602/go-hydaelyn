package cases

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Viking602/go-hydaelyn/eval"
)

func TestValidateCase(t *testing.T) {
	t.Run("valid case", func(t *testing.T) {
		err := ValidateCase(eval.EvalCase{
			SchemaVersion: eval.EvalCaseSchemaVersion,
			ID:            "case-1",
			Suite:         "fixture",
			Pattern:       "deepsearch",
			Profiles:      &eval.EvalCaseProfiles{Supervisor: "supervisor", Worker: "worker", Workers: []string{"worker-2", "worker-3"}},
			Tools:         []string{"fixture_search", "calculator"},
			Fixtures:      &eval.EvalCaseFixtures{CorpusIDs: []string{"policy-001"}, Paths: []string{"fixtures/corpus"}},
			Thresholds:    &eval.EvalCaseThresholds{AnswerCorrectness: 0.9, Groundedness: 0.8, CitationPrecision: 0.9, CitationRecall: 0.8, ToolPrecision: 0.8, ToolRecall: 0.8, ToolArgAccuracy: 0.8, SynthesisInputCoverage: 0.9, RetrySuccessRate: 1},
			Limits:        &eval.EvalCaseLimits{MaxToolCalls: 3, MaxLatencyMs: 1000, MaxTokens: 500},
		})
		if err != nil {
			t.Fatalf("ValidateCase() error = %v", err)
		}
	})

	t.Run("invalid threshold", func(t *testing.T) {
		err := ValidateCase(eval.EvalCase{
			SchemaVersion: eval.EvalCaseSchemaVersion,
			ID:            "case-1",
			Suite:         "fixture",
			Pattern:       "deepsearch",
			Thresholds:    &eval.EvalCaseThresholds{Groundedness: 1.2},
		})
		if err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("worker profiles cannot be empty", func(t *testing.T) {
		err := ValidateCase(eval.EvalCase{
			SchemaVersion: eval.EvalCaseSchemaVersion,
			ID:            "case-1",
			Suite:         "fixture",
			Pattern:       "deepsearch",
			Profiles:      &eval.EvalCaseProfiles{Supervisor: "supervisor", Workers: []string{"worker-a", ""}},
		})
		if err == nil {
			t.Fatal("expected worker profile validation error")
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
	path := filepath.Join("..", "..", "testdata", "corpus")
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

func TestDiscoverCasePaths(t *testing.T) {
	root := t.TempDir()
	caseDir := filepath.Join(root, "deepsearch")
	if err := os.MkdirAll(filepath.Join(caseDir, "fixtures"), 0o755); err != nil {
		t.Fatalf("mkdir case dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(caseDir, "runs", "old"), 0o755); err != nil {
		t.Fatalf("mkdir runs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "b.json"), []byte(`{"schemaVersion":"1.0","id":"b","suite":"deepsearch","pattern":"deepsearch","provider":{"scriptPath":"scripts/provider.json"}}`), 0o644); err != nil {
		t.Fatalf("write b case: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "a.json"), []byte(`{"schemaVersion":"1.0","id":"a","suite":"deepsearch","pattern":"deepsearch","provider":{"scriptPath":"scripts/provider.json"}}`), 0o644); err != nil {
		t.Fatalf("write a case: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "fixtures", "provider.json"), []byte(`[{"kind":"done"}]`), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "runs", "old", "score.json"), []byte(`{"overallScore":1}`), 0o644); err != nil {
		t.Fatalf("write old score: %v", err)
	}

	paths, err := DiscoverCasePaths(root)
	if err != nil {
		t.Fatalf("DiscoverCasePaths() error = %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("len(paths) = %d, want 2", len(paths))
	}
	if filepath.Base(paths[0]) != "a.json" || filepath.Base(paths[1]) != "b.json" {
		t.Fatalf("unexpected path order: %#v", paths)
	}
}
