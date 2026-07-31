package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/Viking602/venat/internal/core/model"
)

func TestVersionPrintsSourceBuildVersion(t *testing.T) {
	// Given
	var stdout, stderr bytes.Buffer

	// When
	if err := Execute(context.Background(), []string{"version"}, &stdout, &stderr); err != nil {
		t.Fatalf("version error = %v", err)
	}

	// Then
	if got := strings.TrimSpace(stdout.String()); got != "devel" {
		t.Fatalf("version = %q, want devel", got)
	}
}

func TestHelpListsTopLevelCommandsAndSourceBuildVersion(t *testing.T) {
	// Given
	var stdout, stderr bytes.Buffer

	// When
	if err := Execute(context.Background(), []string{"help"}, &stdout, &stderr); err != nil {
		t.Fatalf("help error = %v", err)
	}

	// Then
	if !strings.Contains(stdout.String(), "inspect-events") {
		t.Fatalf("help should advertise inspect-events, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "(devel)") {
		t.Fatalf("help should display source build version, got %s", stdout.String())
	}
}

func TestResolveBuildVersionUsesModuleVersion(t *testing.T) {
	tests := []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{name: "missing build info", info: nil, want: "devel"},
		{name: "source build", info: &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, want: "devel"},
		{name: "source build pseudo-version", info: &debug.BuildInfo{Main: debug.Module{Version: "v0.9.1-0.20260705053004-95e511035235+dirty"}}, want: "devel"},
		{name: "installed module version", info: &debug.BuildInfo{Main: debug.Module{Version: "v0.10.0", Sum: "h1:module-checksum"}}, want: "v0.10.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When
			got := resolveBuildVersion(tt.info)

			// Then
			if got != tt.want {
				t.Fatalf("resolveBuildVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInspectEventsFiltersByTask(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.json")
	events := []model.Event{
		{RunID: "run-1", TaskID: "task-1", Sequence: 1, Type: model.EventTaskCreated},
		{RunID: "run-1", TaskID: "task-2", Sequence: 2, Type: model.EventTaskCreated},
		{RunID: "run-1", TaskID: "task-1", Sequence: 3, Type: model.EventTaskCompleted},
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
