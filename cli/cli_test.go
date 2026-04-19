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
	if !strings.Contains(stdout.String(), "\"kind\": \"events\"") {
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
