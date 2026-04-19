package benchmark

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCatalogValidatesAndLoadsSummary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")
	if err := os.WriteFile(path, []byte(`{
  "version": "1",
  "benchmarks": [
    {
      "id": "memorybench",
      "name": "MemoryBench",
      "officialRepoUrl": "https://example.com/memorybench.git",
      "officialRef": "abc123",
      "primaryMetrics": ["qaAccuracy"],
      "smokeCommands": ["echo smoke"]
    }
  ],
  "lanes": [
    {
      "id": "openai_flagship",
      "provider": "openai",
      "model": "gpt-test"
    }
  ]
}`), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	catalog, err := LoadCatalog(path)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	if catalog.DefaultOutputDir == "" {
		t.Fatal("expected default output dir")
	}
	summary := catalog.Summary()
	if summary["benchmarkCount"].(int) != 1 {
		t.Fatalf("unexpected benchmark count: %#v", summary["benchmarkCount"])
	}
}

func TestResolveCommandsExpandsTemplates(t *testing.T) {
	t.Parallel()
	commands, err := ResolveCommands([]string{"echo {{.Benchmark.ID}} {{.Lane.Model}} {{.TrialCount}}"}, TemplateData{
		Benchmark:  BenchmarkSpec{ID: "memorybench"},
		Lane:       LaneSpec{Model: "gpt-test"},
		TrialCount: 4,
	})
	if err != nil {
		t.Fatalf("resolve commands: %v", err)
	}
	if len(commands) != 1 || commands[0] != "echo memorybench gpt-test 4" {
		t.Fatalf("unexpected commands: %#v", commands)
	}
}
