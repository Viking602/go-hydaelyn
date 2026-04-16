package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/host"
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
	if err := Execute(context.Background(), []string{"inspect", "--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("inspect error = %v", err)
	}
	if !strings.Contains(stdout.String(), "teamId") {
		t.Fatalf("expected inspect output to include teamId, got %s", stdout.String())
	}

	stdout.Reset()
	if err := Execute(context.Background(), []string{"replay", "--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("replay error = %v", err)
	}
	if !strings.Contains(stdout.String(), "\"status\": \"completed\"") {
		t.Fatalf("expected replay output to include completed status, got %s", stdout.String())
	}
}
