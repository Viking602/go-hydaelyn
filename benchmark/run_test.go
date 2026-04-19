package benchmark

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRunCreatesArtifactsAndComparison(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	catalogPath := filepath.Join(workspace, "catalog.json")
	scoresPath := filepath.Join(workspace, "scores.json")
	catalog := `{
  "version": "1",
  "defaultOutputDir": "results",
  "benchmarks": [
    {
      "id": "memorybench",
      "name": "MemoryBench",
      "officialPaperUrl": "https://example.com/memorybench-paper",
      "primaryMetrics": ["qaAccuracy", "abstentionAccuracy"],
      "setupCommands": ["` + testEchoCommand("setup") + `"],
      "smokeCommands": ["` + testEchoCommand("run") + `"],
      "baselines": [
        {
          "label": "official-2026-04",
          "default": true,
          "sourceUrl": "https://example.com/leaderboard",
          "scores": {
            "qaAccuracy": 0.5
          }
        }
      ]
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
	if err := os.WriteFile(scoresPath, []byte(`{"scores":{"qaAccuracy":0.75,"abstentionAccuracy":0.9}}`), 0o644); err != nil {
		t.Fatalf("write scores: %v", err)
	}
	result, err := Run(context.Background(), RunOptions{
		CatalogPath: catalogPath,
		BenchmarkID: "memorybench",
		LaneID:      "openai_flagship",
		Mode:        "smoke",
		Workspace:   workspace,
		ScoresFile:  scoresPath,
		TrialCount:  4,
	})
	if err != nil {
		t.Fatalf("run benchmark: %v", err)
	}
	if result.Scores["qaAccuracy"] != 0.75 {
		t.Fatalf("unexpected score: %#v", result.Scores)
	}
	if result.Comparison.BaselineLabel != "official-2026-04" {
		t.Fatalf("unexpected baseline: %#v", result.Comparison)
	}
	for _, name := range []string{"run.json", "comparison.json", "comparison.md"} {
		if _, err := os.Stat(filepath.Join(result.OutputDir, name)); err != nil {
			t.Fatalf("missing artifact %s: %v", name, err)
		}
	}
}

func TestParseScoreValues(t *testing.T) {
	t.Parallel()
	scores, err := ParseScoreValues([]string{"pass^1=0.5", "pass^4=0.8"})
	if err != nil {
		t.Fatalf("parse scores: %v", err)
	}
	if len(scores) != 2 || scores["pass^4"] != 0.8 {
		t.Fatalf("unexpected scores: %#v", scores)
	}
}

func testEchoCommand(message string) string {
	if runtime.GOOS == "windows" {
		return "Write-Output '" + message + "'"
	}
	return "printf '" + message + "\\n'"
}
