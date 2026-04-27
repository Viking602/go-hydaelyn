package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/orchestrator"
)

func TestVersionPrintsBuildString(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{"version"}, &stdout, &stderr); err != nil {
		t.Fatalf("version error = %v", err)
	}
	if strings.TrimSpace(stdout.String()) == "" {
		t.Fatal("version produced no output")
	}
}

func TestHelpListsTopLevelCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{"help"}, &stdout, &stderr); err != nil {
		t.Fatalf("help error = %v", err)
	}
	if !strings.Contains(stdout.String(), "inspect-events") {
		t.Fatalf("help should advertise inspect-events, got %s", stdout.String())
	}
}

func TestInspectEventsFiltersByTask(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.json")
	events := []orchestrator.Event{
		{RunID: "run-1", TaskID: "task-1", Sequence: 1, Type: orchestrator.EventTaskCreated},
		{RunID: "run-1", TaskID: "task-2", Sequence: 2, Type: orchestrator.EventTaskCreated},
		{RunID: "run-1", TaskID: "task-1", Sequence: 3, Type: orchestrator.EventTaskCompleted},
	}
	payload, err := json.Marshal(events)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(eventsPath, payload, 0o644); err != nil {
		t.Fatalf("write events: %v", err)
	}
	var stdout, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{"inspect-events", "--events", eventsPath, "--task", "task-1"}, &stdout, &stderr); err != nil {
		t.Fatalf("inspect-events error = %v", err)
	}
	var parsed struct {
		EventCount int `json:"eventCount"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		t.Fatalf("decode output: %v output=%s", err, stdout.String())
	}
	if parsed.EventCount != 2 {
		t.Fatalf("expected 2 events for task-1, got %d (output=%s)", parsed.EventCount, stdout.String())
	}
}
