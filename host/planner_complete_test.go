package host

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/internal/plugin"
	"github.com/Viking602/go-hydaelyn/planner"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

// TestReviewActionCompleteTerminatesTeam verifies that a planner returning
// ReviewActionComplete drives the run state to StatusCompleted rather than
// being silently treated as a continue signal. Before PR 1 the runtime
// folded Complete and Continue into the same branch, so a planner that
// declared the run finished would see the scheduler try to advance another
// phase instead of halting.
func TestReviewActionCompleteTerminatesTeam(t *testing.T) {
	driver := storage.NewMemoryDriver()
	runner := New(Config{Storage: driver})
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor})

	if err := runner.RegisterPlugin(plugin.Spec{
		Type: plugin.TypePlanner,
		Name: "complete-planner",
		Component: fakePlanner{
			reviewFn: func(_ context.Context, _ planner.ReviewInput) (planner.ReviewDecision, error) {
				return planner.ReviewDecision{Action: planner.ReviewActionComplete, Reason: "goal met"}, nil
			},
		},
	}); err != nil {
		t.Fatalf("RegisterPlugin(planner) error = %v", err)
	}

	state := team.RunState{
		ID:       "team-complete",
		Pattern:  "linear",
		Status:   team.StatusRunning,
		Phase:    team.PhaseResearch,
		Planning: &team.PlanningState{PlannerName: "complete-planner", PlanVersion: 1},
		Supervisor: team.AgentInstance{
			ID:          "supervisor",
			Role:        team.RoleSupervisor,
			ProfileName: "supervisor",
		},
		Tasks: []team.Task{{
			ID:     "task-1",
			Kind:   team.TaskKindResearch,
			Status: team.TaskStatusCompleted,
		}},
	}
	state.Normalize()
	// reviewPlannedTeam drives finishReviewedTeam -> saveTeam -> SaveCAS, so
	// the team must be persisted first to establish a baseline version.
	if err := driver.Teams().Save(context.Background(), state); err != nil {
		t.Fatalf("driver.Teams().Save() error = %v", err)
	}
	reloaded, err := driver.Teams().Load(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("driver.Teams().Load() error = %v", err)
	}
	state = reloaded

	next, progressed, terminal, err := runner.reviewPlannedTeam(context.Background(), state)
	if err != nil {
		t.Fatalf("reviewPlannedTeam() error = %v", err)
	}
	if !progressed {
		t.Fatalf("expected Complete to mark progress so the driver exits, got progressed=false")
	}
	if !terminal {
		t.Fatalf("expected Complete to mark the run terminal, got terminal=false")
	}
	if next.Status != team.StatusCompleted {
		t.Fatalf("expected StatusCompleted, got %q", next.Status)
	}
	if next.Planning == nil || next.Planning.LastAction != string(planner.ReviewActionComplete) {
		t.Fatalf("expected planning.LastAction=complete, got %#v", next.Planning)
	}
	if next.Result == nil || next.Result.Summary != "goal met" {
		t.Fatalf("expected reason surfaced as Summary (not Error) on completion, got %#v", next.Result)
	}
	if next.Result.Error != "" {
		t.Fatalf("expected empty Error on completion, got %q", next.Result.Error)
	}
}
