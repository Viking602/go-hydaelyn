package host

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/patterns/deepsearch"
	"github.com/Viking602/go-hydaelyn/planner"
	"github.com/Viking602/go-hydaelyn/plugin"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

func TestRuntimePausedTeamRecordsApprovalRequestedEvent(t *testing.T) {
	runtime := New(Config{})
	runtime.RegisterProvider("team-fake", teamFakeProvider{})
	runtime.RegisterPattern(deepsearch.New())
	runtime.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "team-fake", Model: "test"})
	runtime.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "team-fake", Model: "test"})
	if err := runtime.RegisterPlugin(plugin.Spec{
		Type: plugin.TypePlanner,
		Name: "ask-human",
		Component: fakePlanner{
			planFn: func(_ context.Context, _ planner.PlanRequest) (planner.Plan, error) {
				return planner.Plan{
					Goal: "needs approval",
					Tasks: []planner.TaskSpec{
						{ID: "task-1", Kind: string(team.TaskKindResearch), Title: "branch", Input: "branch", RequiredRole: team.RoleResearcher},
					},
				}, nil
			},
			reviewFn: func(_ context.Context, _ planner.ReviewInput) (planner.ReviewDecision, error) {
				return planner.ReviewDecision{Action: planner.ReviewActionAskHuman, Reason: "need approval"}, nil
			},
		},
	}); err != nil {
		t.Fatalf("RegisterPlugin() error = %v", err)
	}

	state, err := runtime.StartTeam(context.Background(), StartTeamRequest{
		Pattern:           "deepsearch",
		Planner:           "ask-human",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input:             map[string]any{"query": "approval"},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	if state.Status != team.StatusPaused {
		t.Fatalf("expected paused state, got %#v", state)
	}
	events, err := runtime.storage.Events().List(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	foundApproval := false
	for _, event := range events {
		if event.Type == storage.EventApprovalRequested {
			foundApproval = true
		}
	}
	if !foundApproval {
		t.Fatalf("expected approval requested event, got %#v", events)
	}
	replayed, err := runtime.ReplayTeamState(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("ReplayTeamState() error = %v", err)
	}
	if replayed.Status != team.StatusPaused {
		t.Fatalf("expected replayed paused state, got %#v", replayed)
	}
}

func TestRuntimeResumeTeamContinuesAfterPause(t *testing.T) {
	runtime := New(Config{})
	runtime.RegisterProvider("team-fake", teamFakeProvider{})
	runtime.RegisterPattern(deepsearch.New())
	runtime.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "team-fake", Model: "test"})
	runtime.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "team-fake", Model: "test"})
	reviewCount := 0
	if err := runtime.RegisterPlugin(plugin.Spec{
		Type: plugin.TypePlanner,
		Name: "resume-after-approval",
		Component: fakePlanner{
			planFn: func(_ context.Context, _ planner.PlanRequest) (planner.Plan, error) {
				return planner.Plan{
					Goal: "approval then continue",
					Tasks: []planner.TaskSpec{
						{ID: "task-1", Kind: string(team.TaskKindResearch), Title: "branch", Input: "branch", RequiredRole: team.RoleResearcher},
					},
				}, nil
			},
			reviewFn: func(_ context.Context, _ planner.ReviewInput) (planner.ReviewDecision, error) {
				reviewCount++
				if reviewCount == 1 {
					return planner.ReviewDecision{Action: planner.ReviewActionAskHuman, Reason: "need approval"}, nil
				}
				return planner.ReviewDecision{Action: planner.ReviewActionContinue}, nil
			},
		},
	}); err != nil {
		t.Fatalf("RegisterPlugin() error = %v", err)
	}

	state, err := runtime.StartTeam(context.Background(), StartTeamRequest{
		Pattern:           "deepsearch",
		Planner:           "resume-after-approval",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input:             map[string]any{"query": "approval"},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	if state.Status != team.StatusPaused {
		t.Fatalf("expected paused state, got %#v", state)
	}
	resumed, err := runtime.ResumeTeam(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("ResumeTeam() error = %v", err)
	}
	if resumed.Status != team.StatusCompleted {
		t.Fatalf("expected resumed run to complete, got %#v", resumed)
	}
}
