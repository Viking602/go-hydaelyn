package benchcli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Viking602/go-hydaelyn/benchmark"
	"github.com/Viking602/go-hydaelyn/evaluation"
	"github.com/Viking602/go-hydaelyn/provider"
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

func TestBenchCLIRunLive(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	casePath := filepath.Join(workspace, "case.json")
	if err := os.WriteFile(casePath, []byte(`{"schemaVersion":"1.0","id":"live-case","suite":"live","pattern":"deepsearch","input":{"prompt":"Say alpha beta"},"expected":{"mustInclude":["alpha","beta"]}}`), 0o644); err != nil {
		t.Fatalf("write case: %v", err)
	}
	catalogPath := filepath.Join(workspace, "catalog.json")
	if err := os.WriteFile(catalogPath, []byte(`{"version":"1","benchmarks":[{"id":"bench","name":"Bench","officialPaperUrl":"https://example.com/paper","primaryMetrics":["overallScore"],"smokeCommands":["Write-Output 'noop'"]}],"lanes":[{"id":"nightly","provider":"cli-live-test","model":"model-x"}]}`), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	original, existed := benchmarkLiveProviderFactory("cli-live-test")
	benchmarkSetLiveProviderFactory("cli-live-test", func(benchmark.LaneSpec) (provider.Driver, error) {
		return benchmarkTestDriver{events: []provider.Event{{Kind: provider.EventTextDelta, Text: "alpha beta", Usage: provider.Usage{InputTokens: 9, OutputTokens: 4, TotalTokens: 13}}, {Kind: provider.EventDone, StopReason: provider.StopReasonComplete, Usage: provider.Usage{InputTokens: 9, OutputTokens: 4, TotalTokens: 13}}}}, nil
	})
	t.Cleanup(func() {
		if existed {
			benchmarkSetLiveProviderFactory("cli-live-test", original)
			return
		}
		benchmarkDeleteLiveProviderFactory("cli-live-test")
	})
	var stdout, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{"run-live", "--catalog", catalogPath, "--case", casePath, "--lane", "nightly"}, &stdout, &stderr); err != nil {
		t.Fatalf("benchcli run-live error = %v stderr=%s", err, stderr.String())
	}
	var run evaluation.EvalRun
	if err := json.Unmarshal(stdout.Bytes(), &run); err != nil {
		t.Fatalf("decode eval run: %v output=%s", err, stdout.String())
	}
	if run.Mode != evaluation.EvalRunModeLive || run.Status != evaluation.EvalRunStatusCompleted {
		t.Fatalf("unexpected eval run: %#v", run)
	}
}

type benchmarkTestDriver struct {
	events []provider.Event
}

func (d benchmarkTestDriver) Metadata() provider.Metadata {
	return provider.Metadata{Name: "cli-live-test", Version: "test-v1", Models: []string{"model-x"}}
}

func (d benchmarkTestDriver) Stream(context.Context, provider.Request) (provider.Stream, error) {
	return provider.NewSliceStream(d.events), nil
}

func benchmarkLiveProviderFactory(name string) (func(benchmark.LaneSpec) (provider.Driver, error), bool) {
	factory, ok := benchmark.LiveLaneProviderFactory(name)
	return factory, ok
}

func benchmarkSetLiveProviderFactory(name string, factory func(benchmark.LaneSpec) (provider.Driver, error)) {
	benchmark.SetLiveLaneProviderFactory(name, factory)
}

func benchmarkDeleteLiveProviderFactory(name string) {
	benchmark.DeleteLiveLaneProviderFactory(name)
}
