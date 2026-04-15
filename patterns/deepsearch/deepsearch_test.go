package deepsearch

import (
	"context"
	"testing"

	"hydaelyn/team"
)

func TestPatternCreatesResearchTasksThenVerificationPhase(t *testing.T) {
	pattern := New()
	state, err := pattern.Start(context.Background(), team.StartRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"research-a", "research-b"},
		Input: map[string]any{
			"query":               "agent frameworks",
			"subqueries":          []string{"architecture", "verification"},
			"requireVerification": true,
		},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if state.Phase != team.PhaseResearch {
		t.Fatalf("expected research phase, got %s", state.Phase)
	}
	if len(state.Tasks) != 2 {
		t.Fatalf("expected 2 research tasks, got %d", len(state.Tasks))
	}
	for idx := range state.Tasks {
		state.Tasks[idx].Status = team.TaskStatusCompleted
		state.Tasks[idx].Result = &team.Result{
			Summary: state.Tasks[idx].Input,
		}
	}
	next, err := pattern.Advance(context.Background(), state)
	if err != nil {
		t.Fatalf("Advance() error = %v", err)
	}
	if next.Phase != team.PhaseVerify {
		t.Fatalf("expected verification phase, got %s", next.Phase)
	}
	if len(next.Tasks) != 4 {
		t.Fatalf("expected research + verifier tasks to be present, got %d", len(next.Tasks))
	}
}
