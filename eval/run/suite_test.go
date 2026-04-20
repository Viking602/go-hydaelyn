package run

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/eval"
)

func TestRunCaseDirectory(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	caseRoot := filepath.Join(workspace, "cases")
	if err := os.MkdirAll(caseRoot, 0o755); err != nil {
		t.Fatalf("mkdir case root: %v", err)
	}
	writeScript(t, filepath.Join(workspace, "scripts", "provider.json"), `[
  {"kind":"text_delta","text":"alpha"},
  {"kind":"done","stopReason":"complete","usage":{"inputTokens":2,"outputTokens":1,"totalTokens":3}}
]`)
	caseA := `{
  "schemaVersion": "1.0",
  "id": "case-a",
  "suite": "agent_core",
  "pattern": "deepsearch",
  "provider": {"scriptPath": "scripts/provider.json"},
  "expected": {"mustInclude": ["alpha"]}
}`
	caseB := `{
  "schemaVersion": "1.0",
  "id": "case-b",
  "suite": "agent_core",
  "pattern": "deepsearch",
  "provider": {"scriptPath": "scripts/provider.json"},
  "expected": {"mustInclude": ["beta"]}
}`
	if err := os.WriteFile(filepath.Join(caseRoot, "case-a.json"), []byte(strings.TrimSpace(caseA)), 0o644); err != nil {
		t.Fatalf("write case-a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caseRoot, "case-b.json"), []byte(strings.TrimSpace(caseB)), 0o644); err != nil {
		t.Fatalf("write case-b: %v", err)
	}

	runner := NewRunner(RunnerOptions{
		Workspace:  workspace,
		OutputRoot: filepath.Join(workspace, "out"),
		Now:        fixedNow,
	})
	suite, err := runner.RunCaseDirectory(context.Background(), caseRoot)
	if err != nil {
		t.Fatalf("RunCaseDirectory() error = %v", err)
	}
	if suite.TotalCases != 2 || suite.Passed != 1 || suite.Failed != 1 {
		t.Fatalf("unexpected suite counts: %#v", suite)
	}
	if suite.Pass {
		t.Fatalf("expected suite pass=false, got %#v", suite)
	}
	if suite.ReleaseDecision != eval.ReleaseDecisionNoGo {
		t.Fatalf("expected suite release decision No-Go, got %#v", suite)
	}
	for _, name := range []string{"suite.json", "cases.json", "score.json", "capability.report.json", "summary.md"} {
		if _, err := os.Stat(filepath.Join(suite.OutputDir, name)); err != nil {
			t.Fatalf("missing suite artifact %s: %v", name, err)
		}
	}
}
