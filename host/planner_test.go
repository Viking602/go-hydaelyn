package host

import (
	"context"
	"fmt"
	"testing"

	"github.com/Viking602/go-hydaelyn/pattern/deepsearch"
	"github.com/Viking602/go-hydaelyn/planner"
	"github.com/Viking602/go-hydaelyn/internal/plugin"
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

func TestPlannerGeneratesPlanAndAssignsWorkerByCapability(t *testing.T) {
	runner := New(Config{})
	runner.RegisterProvider("team-fake", teamFakeProvider{})
	runner.RegisterPattern(deepsearch.New())
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "team-fake", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "research-search", Role: team.RoleResearcher, Provider: "team-fake", Model: "test", ToolNames: []string{"search"}})
	runner.RegisterProfile(team.Profile{Name: "research-fetch", Role: team.RoleResearcher, Provider: "team-fake", Model: "test", ToolNames: []string{"fetch"}})

	if err := runner.RegisterPlugin(plugin.Spec{
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

	state, err := runner.StartTeam(context.Background(), StartTeamRequest{
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

func TestPlannerReviewCanTriggerReplan(t *testing.T) {
	runner := New(Config{})
	runner.RegisterProvider("team-fake", teamFakeProvider{})
	runner.RegisterPattern(deepsearch.New())
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "team-fake", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "team-fake", Model: "test", ToolNames: []string{"search"}})

	reviewCount := 0
	replanCount := 0
	if err := runner.RegisterPlugin(plugin.Spec{
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

	state, err := runner.StartTeam(context.Background(), StartTeamRequest{
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

func TestPlannerAskHumanPausesTeam(t *testing.T) {
	runner := New(Config{})
	runner.RegisterProvider("team-fake", teamFakeProvider{})
	runner.RegisterPattern(deepsearch.New())
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "team-fake", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "team-fake", Model: "test"})

	if err := runner.RegisterPlugin(plugin.Spec{
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

	state, err := runner.StartTeam(context.Background(), StartTeamRequest{
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

func TestMultiAgentCollaboration_ReplanRejectsLateSupersededResult(t *testing.T) {
	runner := New(Config{})
	current := team.RunState{
		ID:      "team-replan-stale",
		Pattern: "deepsearch",
		Status:  team.StatusRunning,
		Phase:   team.PhaseResearch,
		Workers: []team.AgentInstance{{ID: "worker-1", Role: team.RoleResearcher, ProfileName: "researcher"}},
		Tasks: []team.Task{
			{ID: "task-1", Kind: team.TaskKindResearch, Title: "original", Input: "old branch", AssigneeAgentID: "worker-1", FailurePolicy: team.FailurePolicyFailFast, SessionID: "stale-session", Status: team.TaskStatusRunning},
			{ID: "task-2", Kind: team.TaskKindResearch, Title: "dropped", Input: "drop me", AssigneeAgentID: "worker-1", FailurePolicy: team.FailurePolicyFailFast, Status: team.TaskStatusPending},
		},
		Planning: &team.PlanningState{PlannerName: "replan", PlanVersion: 1},
	}
	current.Normalize()

	next, err := runner.replanTeam(context.Background(), fakePlanner{
		replanFn: func(_ context.Context, input planner.ReplanInput) (planner.Plan, error) {
			if got := len(input.State.Tasks); got != 2 {
				t.Fatalf("expected current tasks in replan input, got %d", got)
			}
			return planner.Plan{
				Goal: "replanned",
				Tasks: []planner.TaskSpec{
					{ID: "task-1", Kind: string(team.TaskKindResearch), Title: "replacement", Input: "new branch", AssigneeAgentID: "worker-1", FailurePolicy: team.FailurePolicyFailFast},
					{ID: "task-3", Kind: string(team.TaskKindResearch), Title: "fresh", Input: "fresh branch", AssigneeAgentID: "worker-1", FailurePolicy: team.FailurePolicyFailFast},
				},
			}, nil
		},
	}, current)
	if err != nil {
		t.Fatalf("replanTeam() error = %v", err)
	}
	if next.Planning.PlanVersion != 2 {
		t.Fatalf("expected plan version 2 after replan, got %#v", next.Planning)
	}
	if len(next.Tasks) != 3 {
		t.Fatalf("expected replaced task, aborted stale task, and new task, got %#v", next.Tasks)
	}
	if next.Tasks[0].Version != current.Tasks[0].Version+1 || next.Tasks[0].Status != team.TaskStatusPending || next.Tasks[0].Input != "new branch" || next.Tasks[0].SessionID != "" {
		t.Fatalf("expected task-1 to be superseded in place, got %#v", next.Tasks[0])
	}
	if next.Tasks[1].Status != team.TaskStatusAborted || next.Tasks[1].Error != eventReasonSupersededByReplan {
		t.Fatalf("expected dropped task to be aborted, got %#v", next.Tasks[1])
	}
	if next.Tasks[2].ID != "task-3" || next.Tasks[2].Status != team.TaskStatusPending {
		t.Fatalf("expected fresh replanned task to be appended, got %#v", next.Tasks[2])
	}

	lateReplacement := current.Tasks[0]
	lateReplacement.Status = team.TaskStatusCompleted
	lateReplacement.Result = &team.Result{Summary: "late stale result"}
	ignored, applied, published, _, err := runner.applyTaskOutcome(context.Background(), next, 0, lateReplacement)
	if err != nil {
		t.Fatalf("applyTaskOutcome() error = %v", err)
	}
	if applied || published {
		t.Fatalf("expected superseded version result to be ignored, applied=%v published=%v", applied, published)
	}
	if ignored.Tasks[0].Version != next.Tasks[0].Version || ignored.Tasks[0].Result != nil {
		t.Fatalf("expected authoritative task-1 state to remain unchanged, got %#v", ignored.Tasks[0])
	}

	lateDropped := current.Tasks[1]
	lateDropped.Status = team.TaskStatusCompleted
	lateDropped.Result = &team.Result{Summary: "late dropped result"}
	ignored, applied, published, _, err = runner.applyTaskOutcome(context.Background(), next, 1, lateDropped)
	if err != nil {
		t.Fatalf("applyTaskOutcome() error = %v", err)
	}
	if applied || published {
		t.Fatalf("expected aborted stale branch result to be ignored, applied=%v published=%v", applied, published)
	}
	if ignored.Tasks[1].Status != team.TaskStatusAborted || ignored.Tasks[1].Result == nil || ignored.Tasks[1].Result.Error != eventReasonSupersededByReplan {
		t.Fatalf("expected aborted stale branch to remain authoritative, got %#v", ignored.Tasks[1])
	}
}
