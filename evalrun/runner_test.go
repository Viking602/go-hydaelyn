package evalrun

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/evaluation"
)

func TestDeterministicEvalRunner(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	casePath := writeDeterministicCase(t, workspace, `{
  "schemaVersion": "1.0",
  "id": "deterministic-case",
  "suite": "evalrun",
  "pattern": "deepsearch",
  "provider": {
    "scriptPath": "scripts/provider.json"
  },
  "input": {
    "query": "Summarize retention requirements",
    "subqueries": ["Summarize retention requirements"]
  },
  "profiles": {
    "supervisor": "eval-supervisor",
    "worker": "eval-worker"
  },
  "tools": ["fixture_search", "calculator"],
  "fixtures": {
    "corpusIds": ["policy-001"],
    "paths": ["fixtures/corpus"]
  }
}`)
	writeScript(t, filepath.Join(workspace, "scripts", "provider.json"), `[
  {"kind":"text_delta","text":"retention summary from scripted provider"},
  {"kind":"done","stopReason":"complete","usage":{"inputTokens":3,"outputTokens":5,"totalTokens":8}}
]`)
	writeCorpus(t, filepath.Join(workspace, "fixtures", "corpus", "policies.json"), `[
  {"id":"policy-001","date":"2026-04-01T00:00:00Z","text":"retain customer data for 30 days"}
]`)

	runner := NewRunner(RunnerOptions{Workspace: workspace, OutputRoot: filepath.Join(workspace, "out"), Now: fixedNow})
	run, err := runner.Run(context.Background(), casePath)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if run.Status != evaluation.EvalRunStatusCompleted {
		t.Fatalf("status = %s, want completed", run.Status)
	}
	outputDir := filepath.Join(workspace, "out", "runs", "deterministic-case", run.ID, fixedNow().Format(timestampLayout))
	for _, name := range []string{"events.json", "replay.json", "answer.txt", "score.json", "summary.md", "manifest.json", "run.json"} {
		if _, err := os.Stat(filepath.Join(outputDir, name)); err != nil {
			t.Fatalf("missing artifact %s: %v", name, err)
		}
	}
	answer, err := os.ReadFile(filepath.Join(outputDir, "answer.txt"))
	if err != nil {
		t.Fatalf("read answer: %v", err)
	}
	if string(answer) != "retention summary from scripted provider" {
		t.Fatalf("answer = %q", string(answer))
	}
}

func TestDeterministicRepeatability(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	casePath := writeDeterministicCase(t, workspace, `{
  "schemaVersion": "1.0",
  "id": "repeatable-case",
  "suite": "evalrun",
  "pattern": "deepsearch",
  "provider": {
    "scriptPath": "scripts/provider.json"
  },
  "input": {
    "query": "Summarize retention requirements"
  },
  "tools": ["fixture_search"],
  "fixtures": {
    "paths": ["fixtures/corpus"]
  }
}`)
	writeScript(t, filepath.Join(workspace, "scripts", "provider.json"), `[
  {"kind":"text_delta","text":"stable scripted answer"},
  {"kind":"done","stopReason":"complete"}
]`)
	writeCorpus(t, filepath.Join(workspace, "fixtures", "corpus", "policies.json"), `[
  {"id":"policy-001","date":"2026-04-01T00:00:00Z","text":"retain customer data for 30 days"}
]`)

	runnerA := NewRunner(RunnerOptions{Workspace: workspace, OutputRoot: filepath.Join(workspace, "out-a"), Now: fixedNow})
	runA, err := runnerA.Run(context.Background(), casePath)
	if err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	runnerB := NewRunner(RunnerOptions{Workspace: workspace, OutputRoot: filepath.Join(workspace, "out-b"), Now: fixedNow})
	runB, err := runnerB.Run(context.Background(), casePath)
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	manifestA := mustRead(t, filepath.Join(workspace, "out-a", "runs", "repeatable-case", runA.ID, fixedNow().Format(timestampLayout), "manifest.json"))
	manifestB := mustRead(t, filepath.Join(workspace, "out-b", "runs", "repeatable-case", runB.ID, fixedNow().Format(timestampLayout), "manifest.json"))
	if manifestA != manifestB {
		t.Fatalf("manifest mismatch\nA=%s\nB=%s", manifestA, manifestB)
	}
	scoreA := mustRead(t, filepath.Join(workspace, "out-a", "runs", "repeatable-case", runA.ID, fixedNow().Format(timestampLayout), "score.json"))
	scoreB := mustRead(t, filepath.Join(workspace, "out-b", "runs", "repeatable-case", runB.ID, fixedNow().Format(timestampLayout), "score.json"))
	if scoreA != scoreB {
		t.Fatalf("score mismatch\nA=%s\nB=%s", scoreA, scoreB)
	}
}

func TestMalformedCaseRejection(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	casePath := writeDeterministicCase(t, workspace, `{
  "schemaVersion": "1.0",
  "id": "bad-case",
  "suite": "evalrun",
  "pattern": "deepsearch",
  "provider": {}
}`)
	runner := NewRunner(RunnerOptions{Workspace: workspace, OutputRoot: filepath.Join(workspace, "out"), Now: fixedNow})
	_, err := runner.Run(context.Background(), casePath)
	if err == nil || !strings.Contains(err.Error(), "scriptPath or errorKind") {
		t.Fatalf("Run() error = %v, want provider validation error", err)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, "out", "runs", "bad-case")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no partial output, stat err = %v", statErr)
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
}

func writeDeterministicCase(t *testing.T, workspace, content string) string {
	t.Helper()
	path := filepath.Join(workspace, "cases", "case.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir case dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)), 0o644); err != nil {
		t.Fatalf("write case: %v", err)
	}
	return path
}

func writeScript(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir script dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}
}

func writeCorpus(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir corpus dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)), 0o644); err != nil {
		t.Fatalf("write corpus: %v", err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
