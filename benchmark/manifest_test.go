package benchmark

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/evaluation"
)

func TestArtifactManifestIncludesRequiredArtifacts(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	traceDir := filepath.Join(workspace, "trace")
	if err := os.MkdirAll(traceDir, 0o755); err != nil {
		t.Fatalf("mkdir trace dir: %v", err)
	}
	eventsPath := filepath.Join(traceDir, "events.ndjson")
	replayPath := filepath.Join(traceDir, "replay.json")
	evaluationPath := filepath.Join(traceDir, "judge.json")
	for path, content := range map[string]string{
		eventsPath:     "{\"type\":\"event\"}\n",
		replayPath:     "{\"state\":\"ok\"}\n",
		evaluationPath: "{\"score\":0.9}\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write trace artifact %s: %v", path, err)
		}
	}

	result := runBenchmarkFixture(t, workspace, testEchoCommand("run"), RunOptions{
		Trace: TraceBundle{
			EventsPath:      eventsPath,
			ReplayStatePath: replayPath,
			EvaluationPath:  evaluationPath,
		},
	})

	manifest := loadManifest(t, filepath.Join(result.OutputDir, "manifest.json"))
	assertManifestKinds(t, manifest, []evaluation.ArtifactManifestKind{
		evaluation.ArtifactManifestKindEvents,
		evaluation.ArtifactManifestKindReplayState,
		evaluation.ArtifactManifestKindEvaluationReport,
		evaluation.ArtifactManifestKindScore,
		evaluation.ArtifactManifestKindSummary,
		evaluation.ArtifactManifestKindToolCalls,
		evaluation.ArtifactManifestKindModelEvents,
	})

	for _, entry := range manifest.Entries {
		if entry.Path == "" || entry.URI == "" || entry.Checksum == "" {
			t.Fatalf("incomplete manifest entry: %#v", entry)
		}
	}
	if _, err := os.Stat(filepath.Join(result.OutputDir, "manifest.json")); err != nil {
		t.Fatalf("missing manifest.json: %v", err)
	}
}

func TestArtifactManifestRedactsSecrets(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	secret := "sk-test_secret_12345678"
	eventsPath := filepath.Join(workspace, "events.ndjson")
	if err := os.WriteFile(eventsPath, []byte("{\"token\":\""+secret+"\"}\n"), 0o644); err != nil {
		t.Fatalf("write events: %v", err)
	}

	result := runBenchmarkFixture(t, workspace, testEchoCommand(secret), RunOptions{
		Trace: TraceBundle{EventsPath: eventsPath},
	})

	manifest := loadManifest(t, filepath.Join(result.OutputDir, "manifest.json"))
	for _, kind := range []evaluation.ArtifactManifestKind{evaluation.ArtifactManifestKindEvents, evaluation.ArtifactManifestKindToolCalls} {
		entry := manifestEntryByKind(t, manifest, kind)
		if !entry.Redacted {
			t.Fatalf("expected %s to be marked redacted: %#v", kind, entry)
		}
		payload, err := os.ReadFile(filepath.Join(result.OutputDir, filepath.Base(entry.Path)))
		if err != nil {
			t.Fatalf("read canonical artifact %s: %v", kind, err)
		}
		if string(payload) == "" {
			t.Fatalf("expected canonical artifact %s content", kind)
		}
		if containsSecret(string(payload), secret) {
			t.Fatalf("secret leaked in canonical artifact %s: %s", kind, string(payload))
		}
	}
	manifestContent, err := os.ReadFile(filepath.Join(result.OutputDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if containsSecret(string(manifestContent), secret) {
		t.Fatalf("secret leaked in manifest: %s", string(manifestContent))
	}
}

func TestArtifactRetentionClasses(t *testing.T) {
	t.Parallel()

	manifest := GenerateManifest("run-123", []ArtifactInfo{
		{ID: "events", Kind: evaluation.ArtifactManifestKindEvents, Path: "events.ndjson", Content: "{}"},
		{ID: "replay", Kind: evaluation.ArtifactManifestKindReplayState, Path: "replay.json", Content: "{}"},
		{ID: "score", Kind: evaluation.ArtifactManifestKindScore, Path: "score.json", Content: "{}"},
		{ID: "summary", Kind: evaluation.ArtifactManifestKindSummary, Path: "summary.md", Content: "ok"},
		{ID: "tool", Kind: evaluation.ArtifactManifestKindToolCalls, Path: "tool_calls.log", Content: "ok"},
		{ID: "model", Kind: evaluation.ArtifactManifestKindModelEvents, Path: "model_events.log", Content: "ok"},
	})

	want := map[evaluation.ArtifactManifestKind]evaluation.RetentionClass{
		evaluation.ArtifactManifestKindEvents:      evaluation.RetentionClassLongTerm,
		evaluation.ArtifactManifestKindReplayState: evaluation.RetentionClassLongTerm,
		evaluation.ArtifactManifestKindScore:       evaluation.RetentionClassPermanent,
		evaluation.ArtifactManifestKindSummary:     evaluation.RetentionClassPermanent,
		evaluation.ArtifactManifestKindToolCalls:   evaluation.RetentionClassShortTerm,
		evaluation.ArtifactManifestKindModelEvents: evaluation.RetentionClassShortTerm,
	}
	for _, entry := range manifest.Entries {
		if entry.RetentionClass != want[entry.Kind] {
			t.Fatalf("unexpected retention class for %s: got %s want %s", entry.Kind, entry.RetentionClass, want[entry.Kind])
		}
	}
}

func runBenchmarkFixture(t *testing.T, workspace, command string, extra RunOptions) RunResult {
	t.Helper()

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
      "smokeCommands": ["` + command + `"]
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
	options := RunOptions{
		CatalogPath: catalogPath,
		BenchmarkID: "memorybench",
		LaneID:      "openai_flagship",
		Mode:        "smoke",
		Workspace:   workspace,
		ScoresFile:  scoresPath,
		TrialCount:  1,
	}
	options.Trace = extra.Trace
	result, err := Run(context.Background(), options)
	if err != nil {
		t.Fatalf("run benchmark: %v", err)
	}
	return result
}

func loadManifest(t *testing.T, path string) evaluation.ArtifactManifest {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest evaluation.ArtifactManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	return manifest
}

func assertManifestKinds(t *testing.T, manifest evaluation.ArtifactManifest, want []evaluation.ArtifactManifestKind) {
	t.Helper()
	got := map[evaluation.ArtifactManifestKind]bool{}
	for _, entry := range manifest.Entries {
		got[entry.Kind] = true
	}
	for _, kind := range want {
		if !got[kind] {
			t.Fatalf("missing manifest kind %s in %#v", kind, manifest.Entries)
		}
	}
}

func manifestEntryByKind(t *testing.T, manifest evaluation.ArtifactManifest, kind evaluation.ArtifactManifestKind) evaluation.ArtifactManifestEntry {
	t.Helper()
	for _, entry := range manifest.Entries {
		if entry.Kind == kind {
			return entry
		}
	}
	t.Fatalf("missing manifest entry %s", kind)
	return evaluation.ArtifactManifestEntry{}
}

func containsSecret(value, secret string) bool {
	return strings.Contains(value, secret)
}
