package research

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/program"
	"github.com/Viking602/go-hydaelyn/internal/workflow"
)

func TestResearchDriverRespectsBudget(t *testing.T) {
	driver := Driver{
		ProgramLoader: program.MemoryLoader{
			Documents: map[string]program.Document{
				"default": {Name: "default", Body: "keep iterating"},
			},
		},
	}
	state, err := driver.Start(context.Background(), map[string]any{
		"objective": "map the search space",
		"budget":    2,
		"program":   "default",
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if state.Status != workflow.StatusRunning {
		t.Fatalf("expected running state, got %s", state.Status)
	}
	state, err = driver.Resume(context.Background(), state)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	state, err = driver.Resume(context.Background(), state)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if state.Status != workflow.StatusCompleted {
		t.Fatalf("expected completed after budget exhausted, got %s", state.Status)
	}
}
