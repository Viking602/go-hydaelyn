package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/evaluation"
	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

func TestInitAndNewCommandsCreateFiles(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{"init", dir}, &stdout, &stderr); err != nil {
		t.Fatalf("init error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".hydaelyn", "config.json")); err != nil {
		t.Fatalf("expected config file, got %v", err)
	}
	requestPath := filepath.Join(dir, "team.json")
	stdout.Reset()
	if err := Execute(context.Background(), []string{"new", requestPath}, &stdout, &stderr); err != nil {
		t.Fatalf("new error = %v", err)
	}
	if _, err := os.Stat(requestPath); err != nil {
		t.Fatalf("expected request file, got %v", err)
	}
}

func TestRunInspectReplayCommandsWorkOnEventFile(t *testing.T) {
	dir := t.TempDir()
	requestPath := filepath.Join(dir, "team.json")
	eventsPath := filepath.Join(dir, "events.json")
	request := host.StartTeamRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input: map[string]any{
			"query":      "cli run",
			"subqueries": []string{"branch"},
		},
	}
	payload, _ := json.MarshalIndent(request, "", "  ")
	if err := os.WriteFile(requestPath, payload, 0o644); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{"run", "--request", requestPath, "--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run error = %v stderr=%s", err, stderr.String())
	}
	if _, err := os.Stat(eventsPath); err != nil {
		t.Fatalf("expected events file, got %v", err)
	}

	stdout.Reset()
	if err := Execute(context.Background(), []string{"validate", "--request", requestPath}, &stdout, &stderr); err != nil {
		t.Fatalf("validate request error = %v", err)
	}
	if !strings.Contains(stdout.String(), "\"ok\": true") {
		t.Fatalf("expected validate output to confirm success, got %s", stdout.String())
	}

	stdout.Reset()
	if err := Execute(context.Background(), []string{"inspect", "team", "--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("inspect team error = %v", err)
	}
	if !strings.Contains(stdout.String(), "\"tasks\"") {
		t.Fatalf("expected inspect team output to include tasks, got %s", stdout.String())
	}

	stdout.Reset()
	if err := Execute(context.Background(), []string{"inspect", "events", "--events", eventsPath, "--task", "task-1"}, &stdout, &stderr); err != nil {
		t.Fatalf("inspect events error = %v", err)
	}
	if !strings.Contains(stdout.String(), "\"eventCount\"") {
		t.Fatalf("expected inspect events output to include eventCount, got %s", stdout.String())
	}

	stdout.Reset()
	if err := Execute(context.Background(), []string{"validate", "--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("validate events error = %v", err)
	}
	if !strings.Contains(stdout.String(), "\"kind\": \"events\"") || !strings.Contains(stdout.String(), "\"valid\": true") {
		t.Fatalf("expected validate events output, got %s", stdout.String())
	}

	stdout.Reset()
	if err := Execute(context.Background(), []string{"replay", "--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("replay error = %v", err)
	}
	if !strings.Contains(stdout.String(), "\"valid\": true") {
		t.Fatalf("expected replay output to include successful validation, got %s", stdout.String())
	}
}

func TestCLIReplayValidation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.json")
	statePath := filepath.Join(dir, "state.json")
	events := []storage.Event{
		{RunID: "team-1", TeamID: "team-1", Type: storage.EventTeamStarted, Payload: map[string]any{"pattern": "deepsearch"}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "task-1", Type: storage.EventTaskScheduled, Payload: map[string]any{"title": "branch", "status": "pending"}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "task-1", Type: storage.EventTaskCompleted, Payload: map[string]any{"status": "completed", "summary": "done"}},
		{RunID: "team-1", TeamID: "team-1", Type: storage.EventTeamCompleted, Payload: map[string]any{"summary": "done"}},
	}
	authoritativeState := storage.TeamState{
		ID:      "team-1",
		Pattern: "deepsearch",
		Status:  team.StatusCompleted,
		Phase:   team.PhaseComplete,
		Tasks: []team.Task{{
			ID:     "task-1",
			Status: team.TaskStatusCompleted,
			Result: &team.Result{Summary: "done"},
		}},
		Result: &team.Result{Summary: "done"},
	}
	eventsData, _ := json.MarshalIndent(events, "", "  ")
	if err := os.WriteFile(eventsPath, eventsData, 0o644); err != nil {
		t.Fatalf("write events: %v", err)
	}
	stateData, _ := json.MarshalIndent(authoritativeState, "", "  ")
	if err := os.WriteFile(statePath, stateData, 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := Execute(context.Background(), []string{"replay", "--events", eventsPath, "--state", statePath}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("expected replay validation failure")
	}
	if !strings.Contains(err.Error(), "replay validation failed") {
		t.Fatalf("unexpected replay error: %v", err)
	}
	if !strings.Contains(stdout.String(), "\"valid\": false") || !strings.Contains(stdout.String(), "MissingEvent") {
		t.Fatalf("expected replay validation JSON with mismatch details, got %s", stdout.String())
	}
}

func TestCompileAndEvaluateCommandsWork(t *testing.T) {
	dir := t.TempDir()
	recipePath := filepath.Join(dir, "recipe.yaml")
	requestPath := filepath.Join(dir, "team.json")
	eventsPath := filepath.Join(dir, "events.json")
	recipeContent := `
pattern: deepsearch
supervisor_profile: supervisor
worker_profiles: [researcher]
input:
  query: compile example
flow:
  - task:
      id: branch-1
      kind: research
      input: branch
      required_role: researcher
      writes: [branch.one]
      publish: [shared, blackboard]
`
	if err := os.WriteFile(recipePath, []byte(recipeContent), 0o644); err != nil {
		t.Fatalf("write recipe: %v", err)
	}
	request := host.StartTeamRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input: map[string]any{
			"query":      "cli run",
			"subqueries": []string{"branch"},
		},
	}
	payload, _ := json.MarshalIndent(request, "", "  ")
	if err := os.WriteFile(requestPath, payload, 0o644); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{"compile", "--recipe", recipePath}, &stdout, &stderr); err != nil {
		t.Fatalf("compile error = %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"plan\"") || !strings.Contains(stdout.String(), "\"request\"") {
		t.Fatalf("expected compile output to include request and plan, got %s", stdout.String())
	}

	stdout.Reset()
	if err := Execute(context.Background(), []string{"run", "--request", requestPath, "--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run error = %v stderr=%s", err, stderr.String())
	}
	stdout.Reset()
	if err := Execute(context.Background(), []string{"evaluate", "--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("evaluate error = %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"runtimeMetrics\"") {
		t.Fatalf("expected evaluate output to include canonical runtime metrics, got %s", stdout.String())
	}
}

func TestValidateRecipeStrictDataflowReportsIssues(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	recipePath := filepath.Join(dir, "strict-recipe.yaml")
	recipeContent := `
pattern: deepsearch
supervisor_profile: supervisor
worker_profiles: [researcher]
input:
  query: strict dataflow
tasks:
  - id: branch-a
    kind: research
    writes: [shared.branch]
    publish: [blackboard]
  - id: branch-b
    kind: research
    writes: [shared.branch]
    publish: [blackboard]
    exchange_schema: branch.schema
  - id: verify-branch
    kind: verify
    reads: [shared.branch]
  - id: synth-final
    kind: synthesize
    reads: [missing.key]
`
	if err := os.WriteFile(recipePath, []byte(recipeContent), 0o644); err != nil {
		t.Fatalf("write recipe: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{"validate", "--recipe", recipePath, "--strict-dataflow"}, &stdout, &stderr); err != nil {
		t.Fatalf("validate strict-dataflow error = %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"ok\": false") {
		t.Fatalf("expected strict-dataflow validation to fail, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "\"code\": \"ambiguous_producer\"") {
		t.Fatalf("expected ambiguous producer issue, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "\"code\": \"verify_task_has_no_claim_source\"") {
		t.Fatalf("expected verify claim-source issue, got %s", stdout.String())
	}
}

func TestCLIUsesCanonicalEvalOutput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	requestPath := filepath.Join(dir, "team.json")
	eventsPath := filepath.Join(dir, "events.json")
	request := host.StartTeamRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input: map[string]any{
			"query":      "canonical evaluate",
			"subqueries": []string{"branch"},
		},
	}
	data, _ := json.MarshalIndent(request, "", "  ")
	if err := os.WriteFile(requestPath, data, 0o644); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{"run", "--request", requestPath, "--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run error = %v stderr=%s", err, stderr.String())
	}

	stdout.Reset()
	if err := Execute(context.Background(), []string{"evaluate", "--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("evaluate error = %v stderr=%s", err, stderr.String())
	}

	var payload evaluation.ScorePayload
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode score payload: %v output=%s", err, stdout.String())
	}
	if payload.SchemaVersion != evaluation.ScorePayloadSchemaVersion {
		t.Fatalf("unexpected schema version: %#v", payload)
	}
	if payload.RuntimeMetrics == nil {
		t.Fatalf("expected runtime metrics in canonical payload: %#v", payload)
	}
	if !payload.ReplayConsistent {
		t.Fatalf("expected replay consistency in canonical payload: %#v", payload)
	}
	if payload.RunID == "" {
		t.Fatalf("expected canonical payload run id: %#v", payload)
	}
}

func TestCLIReplayRoundTripsStateFromRunOutput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	requestPath := filepath.Join(dir, "team.json")
	eventsPath := filepath.Join(dir, "events.json")
	statePath := filepath.Join(dir, "state.json")
	request := host.StartTeamRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input: map[string]any{
			"query":      "round trip",
			"subqueries": []string{"branch"},
		},
	}
	data, _ := json.MarshalIndent(request, "", "  ")
	if err := os.WriteFile(requestPath, data, 0o644); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{"run", "--request", requestPath, "--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run error = %v stderr=%s", err, stderr.String())
	}
	if err := os.WriteFile(statePath, stdout.Bytes(), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}

	stdout.Reset()
	if err := Execute(context.Background(), []string{"replay", "--events", eventsPath, "--state", statePath}, &stdout, &stderr); err != nil {
		t.Fatalf("replay error = %v output=%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), "\"valid\": true") {
		t.Fatalf("expected replay output to validate round-tripped state, got %s", stdout.String())
	}
}

func TestCLIRunDeterministic(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	caseRoot := filepath.Join(workspace, "cases")
	if err := os.MkdirAll(caseRoot, 0o755); err != nil {
		t.Fatalf("mkdir case root: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "scripts"), 0o755); err != nil {
		t.Fatalf("mkdir scripts dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "scripts", "provider.json"), []byte(`[
  {"kind":"text_delta","text":"alpha"},
  {"kind":"done","stopReason":"complete"}
]`), 0o644); err != nil {
		t.Fatalf("write provider: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caseRoot, "case-a.json"), []byte(`{"schemaVersion":"1.0","id":"case-a","suite":"agent_core","pattern":"deepsearch","provider":{"scriptPath":"scripts/provider.json"},"expected":{"mustInclude":["alpha"]}}`), 0o644); err != nil {
		t.Fatalf("write case-a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caseRoot, "case-b.json"), []byte(`{"schemaVersion":"1.0","id":"case-b","suite":"agent_core","pattern":"deepsearch","provider":{"scriptPath":"scripts/provider.json"},"expected":{"mustInclude":["beta"]}}`), 0o644); err != nil {
		t.Fatalf("write case-b: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{
		"run-deterministic",
		"--case-dir", caseRoot,
		"--workspace", workspace,
		"--output-dir", filepath.Join(workspace, "out"),
	}, &stdout, &stderr); err != nil {
		t.Fatalf("run-deterministic error = %v stderr=%s", err, stderr.String())
	}

	var suite struct {
		TotalCases int    `json:"totalCases"`
		Passed     int    `json:"passed"`
		Failed     int    `json:"failed"`
		OutputDir  string `json:"outputDir"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &suite); err != nil {
		t.Fatalf("decode suite output: %v output=%s", err, stdout.String())
	}
	if suite.TotalCases != 2 || suite.Passed != 1 || suite.Failed != 1 {
		t.Fatalf("unexpected suite output: %#v", suite)
	}
	if suite.OutputDir == "" {
		t.Fatalf("expected output dir in suite output: %#v", suite)
	}
}
