package host

import (
	"context"
	"fmt"
	"testing"

	"github.com/Viking602/go-hydaelyn/patterns/deepsearch"
	"github.com/Viking602/go-hydaelyn/planner"
	"github.com/Viking602/go-hydaelyn/plugin"
	"github.com/Viking602/go-hydaelyn/team"
)

type fakePlanner struct {
	planFn   func(context.Context, planner.PlanRequest) (planner.Plan, error)
	reviewFn func(context.Context, planner.ReviewInput) (planner.ReviewDecision, error)
	replanFn func(context.Context, planner.ReplanInput) (planner.Plan, error)
}

func (f fakePlanner) Plan(ctx context.Context, request planner.PlanRequest) (planner.Plan, error) {
	if f.planFn != nil {
		return f.planFn(ctx, request)
	}
	return planner.Plan{}, fmt.Errorf("unexpected Plan call")
}

func (f fakePlanner) Review(ctx context.Context, input planner.ReviewInput) (planner.ReviewDecision, error) {
	if f.reviewFn != nil {
		return f.reviewFn(ctx, input)
	}
	return planner.ReviewDecision{Action: planner.ReviewActionContinue}, nil
}

func (f fakePlanner) Replan(ctx context.Context, input planner.ReplanInput) (planner.Plan, error) {
	if f.replanFn != nil {
		return f.replanFn(ctx, input)
	}
	return planner.Plan{}, fmt.Errorf("unexpected Replan call")
}

func TestRuntimePlannerGeneratesPlanAndAssignsWorkerByCapability(t *testing.T) {
	runtime := New(Config{})
	runtime.RegisterProvider("team-fake", teamFakeProvider{})
	runtime.RegisterPattern(deepsearch.New())
	runtime.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "team-fake", Model: "test"})
	runtime.RegisterProfile(team.Profile{Name: "research-search", Role: team.RoleResearcher, Provider: "team-fake", Model: "test", ToolNames: []string{"search"}})
	runtime.RegisterProfile(team.Profile{Name: "research-fetch", Role: team.RoleResearcher, Provider: "team-fake", Model: "test", ToolNames: []string{"fetch"}})

	if err := runtime.RegisterPlugin(plugin.Spec{
		Type: plugin.TypePlanner,
		Name: "capability-planner",
		Component: fakePlanner{
			planFn: func(_ context.Context, request planner.PlanRequest) (planner.Plan, error) {
				if request.Template.Name != "deepsearch" {
					t.Fatalf("expected deepsearch template, got %#v", request.Template)
				}
				return planner.Plan{
					Goal: "parallel search",
					Tasks: []planner.TaskSpec{
						{
							ID:                   "task-1",
							Kind:                 string(team.TaskKindResearch),
							Title:                "task one",
							Input:                "first branch",
							RequiredRole:         team.RoleResearcher,
							RequiredCapabilities: []string{"search"},
							FailurePolicy:        team.FailurePolicyFailFast,
						},
						{
							ID:                   "task-2",
							Kind:                 string(team.TaskKindResearch),
							Title:                "task two",
							Input:                "second branch",
							RequiredRole:         team.RoleResearcher,
							RequiredCapabilities: []string{"search"},
							FailurePolicy:        team.FailurePolicyFailFast,
						},
					},
					SuccessCriteria: []string{"complete all search branches"},
				}, nil
			},
		},
	}); err != nil {
		t.Fatalf("RegisterPlugin(planner) error = %v", err)
	}

	state, err := runtime.StartTeam(context.Background(), StartTeamRequest{
		Pattern:           "deepsearch",
		Planner:           "capability-planner",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"research-fetch", "research-search"},
		Input: map[string]any{
			"query":      "parallel search",
			"subqueries": []string{"ignored one", "ignored two"},
		},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	if state.Status != team.StatusCompleted {
		t.Fatalf("expected completed state, got %#v", state)
	}
	if state.Planning == nil || state.Planning.PlannerName != "capability-planner" {
		t.Fatalf("expected planning metadata, got %#v", state.Planning)
	}
	if len(state.Tasks) != 3 {
		t.Fatalf("expected planner tasks plus synth task, got %#v", state.Tasks)
	}
	for _, task := range state.Tasks[:2] {
		if task.AssigneeAgentID != "worker-2" {
			t.Fatalf("expected capability-based assignment to worker-2, got %#v", state.Tasks)
		}
	}
	if state.Tasks[2].Kind != team.TaskKindSynthesize || state.Tasks[2].AssigneeAgentID != "supervisor" {
		t.Fatalf("expected appended synth task on supervisor, got %#v", state.Tasks[2])
	}
}

func TestRuntimePlannerReviewCanTriggerReplan(t *testing.T) {
	runtime := New(Config{})
	runtime.RegisterProvider("team-fake", teamFakeProvider{})
	runtime.RegisterPattern(deepsearch.New())
	runtime.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "team-fake", Model: "test"})
	runtime.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "team-fake", Model: "test", ToolNames: []string{"search"}})

	reviewCount := 0
	replanCount := 0
	if err := runtime.RegisterPlugin(plugin.Spec{
		Type: plugin.TypePlanner,
		Name: "replanning",
		Component: fakePlanner{
			planFn: func(_ context.Context, _ planner.PlanRequest) (planner.Plan, error) {
				return planner.Plan{
					Goal: "replan demo",
					Tasks: []planner.TaskSpec{
						{
							ID:            "task-1",
							Kind:          string(team.TaskKindResearch),
							Title:         "root",
							Input:         "root branch",
							RequiredRole:  team.RoleResearcher,
							FailurePolicy: team.FailurePolicyFailFast,
						},
					},
				}, nil
			},
			reviewFn: func(_ context.Context, input planner.ReviewInput) (planner.ReviewDecision, error) {
				reviewCount++
				if len(input.State.Tasks) == 1 {
					return planner.ReviewDecision{Action: planner.ReviewActionReplan, Reason: "need one more branch"}, nil
				}
				return planner.ReviewDecision{Action: planner.ReviewActionContinue}, nil
			},
			replanFn: func(_ context.Context, input planner.ReplanInput) (planner.Plan, error) {
				replanCount++
				if input.State.Tasks[0].ID != "task-1" {
					t.Fatalf("unexpected replan input %#v", input.State.Tasks)
				}
				return planner.Plan{
					Goal: "replan demo",
					Tasks: []planner.TaskSpec{
						{
							ID:            "task-2",
							Kind:          string(team.TaskKindResearch),
							Title:         "leaf",
							Input:         "leaf branch",
							RequiredRole:  team.RoleResearcher,
							FailurePolicy: team.FailurePolicyFailFast,
						},
					},
				}, nil
			},
		},
	}); err != nil {
		t.Fatalf("RegisterPlugin(planner) error = %v", err)
	}

	state, err := runtime.StartTeam(context.Background(), StartTeamRequest{
		Pattern:           "deepsearch",
		Planner:           "replanning",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input: map[string]any{
			"query": "replan demo",
		},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	if state.Status != team.StatusCompleted {
		t.Fatalf("expected completed state, got %#v", state)
	}
	if reviewCount == 0 || replanCount == 0 {
		t.Fatalf("expected review and replan calls, review=%d replan=%d", reviewCount, replanCount)
	}
	if len(state.Tasks) != 3 || state.Tasks[1].ID != "task-2" {
		t.Fatalf("expected replanned task and synth task to be appended, got %#v", state.Tasks)
	}
	if state.Tasks[2].Kind != team.TaskKindSynthesize {
		t.Fatalf("expected synth task after replanning, got %#v", state.Tasks)
	}
}

func TestRuntimePlannerAskHumanPausesTeam(t *testing.T) {
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
					Goal: "needs human",
					Tasks: []planner.TaskSpec{
						{
							ID:            "task-1",
							Kind:          string(team.TaskKindResearch),
							Title:         "branch",
							Input:         "branch",
							RequiredRole:  team.RoleResearcher,
							FailurePolicy: team.FailurePolicyFailFast,
						},
					},
				}, nil
			},
			reviewFn: func(_ context.Context, _ planner.ReviewInput) (planner.ReviewDecision, error) {
				return planner.ReviewDecision{Action: planner.ReviewActionAskHuman, Reason: "need approval"}, nil
			},
		},
	}); err != nil {
		t.Fatalf("RegisterPlugin(planner) error = %v", err)
	}

	state, err := runtime.StartTeam(context.Background(), StartTeamRequest{
		Pattern:           "deepsearch",
		Planner:           "ask-human",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input: map[string]any{
			"query": "needs human",
		},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	if state.Status != team.StatusPaused {
		t.Fatalf("expected paused state, got %#v", state)
	}
	if state.Result == nil || state.Result.Error == "" {
		t.Fatalf("expected pause reason in result, got %#v", state.Result)
	}
}
