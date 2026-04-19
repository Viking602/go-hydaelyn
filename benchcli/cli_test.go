package benchcli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Viking602/go-hydaelyn/evaluation"
)

func TestBenchCLIUsesCanonicalEvalOutput(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	catalogPath := filepath.Join(workspace, "catalog.json")
	scoresPath := filepath.Join(workspace, "scores.json")
	outputDir := filepath.Join(workspace, "results", "memorybench", "openai_flagship", "run-001")
	catalog := `{
  "version": "1",
  "defaultOutputDir": "results",
  "benchmarks": [
    {
      "id": "memorybench",
      "name": "MemoryBench",
      "officialPaperUrl": "https://example.com/memorybench-paper",
      "primaryMetrics": ["qaAccuracy"],
      "smokeCommands": ["Write-Output 'run'"]
    }
  ],
  "lanes": [
    {
      "id": "openai_flagship",
      "provider": "openai",
      "model": "gpt-test"
    }
  ]
}`
	if err := os.WriteFile(catalogPath, []byte(catalog), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	if err := os.WriteFile(scoresPath, []byte(`{"scores":{"overallScore":0.91,"qaAccuracy":0.91,"latencyMs":1500}}`), 0o644); err != nil {
		t.Fatalf("write scores: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := Execute(context.Background(), []string{
		"run",
		"--catalog", catalogPath,
		"--benchmark", "memorybench",
		"--lane", "openai_flagship",
		"--workspace", workspace,
		"--output-dir", outputDir,
		"--dry-run",
		"--scores-file", scoresPath,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("benchcli run error = %v stderr=%s", err, stderr.String())
	}

	var payload evaluation.ScorePayload
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode score payload: %v output=%s", err, stdout.String())
	}
	if payload.SchemaVersion != evaluation.ScorePayloadSchemaVersion {
		t.Fatalf("unexpected schema version: %#v", payload)
	}
	if payload.RunID != "run-001" {
		t.Fatalf("unexpected run id: %#v", payload)
	}
	if payload.QualityMetrics == nil || payload.QualityMetrics.AnswerCorrectness != 0.91 {
		t.Fatalf("expected canonical quality metrics: %#v", payload)
	}
	if payload.RuntimeMetrics == nil || payload.RuntimeMetrics.EndToEndLatencyMs != 1500 {
		t.Fatalf("expected canonical runtime metrics: %#v", payload)
	}
}
