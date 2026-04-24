package host

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/internal/plugin"
	"github.com/Viking602/go-hydaelyn/pattern/panel"
	"github.com/Viking602/go-hydaelyn/planner"
	"github.com/Viking602/go-hydaelyn/team"
)

func TestBuildPlannedStatePreservesCollaborationContract(t *testing.T) {
	runner := New(Config{})
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor})
	runner.RegisterProfile(team.Profile{Name: "engineer", Role: team.RoleResearcher})
	runner.RegisterProfile(team.Profile{Name: "verifier", Role: team.RoleVerifier})

	state, err := runner.buildPlannedState(team.StartRequest{
		TeamID:            "team-1",
		Pattern:           "deepsearch",
		Planner:           "collab-planner",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"engineer", "verifier"},
		Input: map[string]any{
			"query": "ship collaboration contract",
		},
	}, planner.Plan{
		Goal: "ship collaboration contract",
		Tasks: []planner.TaskSpec{
			{
				ID:               "implement-1",
				Kind:             string(team.TaskKindResearch),
				Stage:            team.TaskStageImplement,
				Title:            "implement contract",
				Input:            "implement contract",
				RequiredRole:     team.RoleResearcher,
				Namespace:        "impl.implement-1",
				VerifierRequired: true,
				FailurePolicy:    team.FailurePolicyFailFast,
			},
			{
				ID:            "legacy-1",
				Kind:          string(team.TaskKindResearch),
				Title:         "legacy task",
				Input:         "legacy task",
				RequiredRole:  team.RoleResearcher,
				FailurePolicy: team.FailurePolicyFailFast,
			},
			{
				ID:            "verify-1",
				Kind:          string(team.TaskKindVerify),
				Title:         "verify contract",
				Input:         "verify contract",
				RequiredRole:  team.RoleVerifier,
				Namespace:     "verify.verify-1",
				FailurePolicy: team.FailurePolicyFailFast,
			},
		},
		VerificationPolicy: planner.VerificationPolicy{Required: true},
	})
	if err != nil {
		t.Fatalf("buildPlannedState() error = %v", err)
	}
	if state.RequireVerification != true {
		t.Fatalf("expected require verification to carry from plan, got %#v", state.RequireVerification)
	}
	if state.Planning == nil || state.Planning.PlanVersion != 1 {
		t.Fatalf("expected planning metadata with version 1, got %#v", state.Planning)
	}
	if len(state.Tasks) != 3 {
		t.Fatalf("expected three runtime tasks, got %#v", state.Tasks)
	}

	implementTask := state.Tasks[0]
	if implementTask.Stage != team.TaskStageImplement {
		t.Fatalf("expected implement stage to carry through, got %#v", implementTask)
	}
	if implementTask.Namespace != "impl.implement-1" {
		t.Fatalf("expected implement namespace to carry through, got %#v", implementTask.Namespace)
	}
	if !implementTask.VerifierRequired {
		t.Fatalf("expected implement task to require verifier, got %#v", implementTask)
	}
	if implementTask.IdempotencyKey != implementTask.ID {
		t.Fatalf("expected default idempotency key %q, got %#v", implementTask.ID, implementTask.IdempotencyKey)
	}
	if implementTask.Version != 1 {
		t.Fatalf("expected default task version 1, got %#v", implementTask.Version)
	}
	if implementTask.AssigneeAgentID != "worker-1" {
		t.Fatalf("expected researcher assignment to worker-1, got %#v", implementTask.AssigneeAgentID)
	}

	legacyTask := state.Tasks[1]
	if legacyTask.Stage != team.TaskStageImplement {
		t.Fatalf("expected legacy research task to normalize to implement stage, got %#v", legacyTask)
	}
	if legacyTask.Namespace != legacyTask.ID {
		t.Fatalf("expected legacy namespace default to task id, got %#v", legacyTask.Namespace)
	}
	if legacyTask.VerifierRequired {
		t.Fatalf("expected legacy task verifier requirement to remain false, got %#v", legacyTask)
	}
	if legacyTask.IdempotencyKey != legacyTask.ID {
		t.Fatalf("expected legacy idempotency key default to task id, got %#v", legacyTask.IdempotencyKey)
	}
	if legacyTask.Version != 1 {
		t.Fatalf("expected legacy task version 1, got %#v", legacyTask.Version)
	}

	verifyTask := state.Tasks[2]
	if verifyTask.Stage != team.TaskStageVerify {
		t.Fatalf("expected verify task to normalize to verify stage, got %#v", verifyTask)
	}
	if verifyTask.Namespace != "verify.verify-1" {
		t.Fatalf("expected verify namespace to carry through, got %#v", verifyTask.Namespace)
	}
	if verifyTask.IdempotencyKey != verifyTask.ID {
		t.Fatalf("expected verify idempotency key default to task id, got %#v", verifyTask.IdempotencyKey)
	}
	if verifyTask.Version != 1 {
		t.Fatalf("expected verify task version 1, got %#v", verifyTask.Version)
	}
	if verifyTask.AssigneeAgentID != "worker-2" {
		t.Fatalf("expected verifier assignment to worker-2, got %#v", verifyTask.AssigneeAgentID)
	}
}

func TestPanelPlannerPathKeepsTaskBoardClaims(t *testing.T) {
	runner := New(Config{})
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor})
	runner.RegisterProfile(team.Profile{Name: "security", Role: team.RoleResearcher})
	runner.RegisterProfile(team.Profile{Name: "frontend", Role: team.RoleResearcher})
	if err := runner.RegisterPlugin(plugin.Spec{
		Type: plugin.TypePlanner,
		Name: "panel-planner",
		Component: fakePlanner{
			planFn: func(_ context.Context, request planner.PlanRequest) (planner.Plan, error) {
				return planner.Plan{
					Goal:  request.Template.Goal,
					Tasks: append([]planner.TaskSpec{}, request.Template.Tasks...),
				}, nil
			},
		},
	}); err != nil {
		t.Fatalf("RegisterPlugin(planner) error = %v", err)
	}

	state, err := runner.startPlannedTeam(context.Background(), panel.New(), team.StartRequest{
		TeamID:            "team-panel",
		Pattern:           "panel",
		Planner:           "panel-planner",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"security", "frontend"},
		Input: map[string]any{
			"query": "launch auth feature",
			"todos": []any{
				map[string]any{"id": "security-review", "title": "review auth threat model", "domain": "security"},
				map[string]any{"id": "ui-review", "title": "review login UI", "domain": "frontend"},
			},
		},
	})
	if err != nil {
		t.Fatalf("startPlannedTeam() error = %v", err)
	}
	if state.InteractionMode != team.InteractionModePanel {
		t.Fatalf("expected panel interaction mode, got %#v", state.InteractionMode)
	}
	if state.TaskBoard == nil || len(state.TaskBoard.Plan.Items) != 2 {
		t.Fatalf("expected planned panel task board, got %#v", state.TaskBoard)
	}
	if state.Tasks[0].AssigneeAgentID != "worker-1" || state.Tasks[1].AssigneeAgentID != "worker-2" {
		t.Fatalf("expected domain-based panel claims to override generic planner assignment, got %#v", state.Tasks)
	}
	if state.TaskBoard.Plan.Items[0].PrimaryAgentID != "worker-1" || state.TaskBoard.Plan.Items[1].PrimaryAgentID != "worker-2" {
		t.Fatalf("expected task board to record claimed experts, got %#v", state.TaskBoard.Plan.Items)
	}
	if state.Tasks[0].ExpectedReportKind != team.ReportKindResearch || state.Tasks[1].ExpectedReportKind != team.ReportKindResearch {
		t.Fatalf("expected planned panel research tasks to require research reports, got %#v", state.Tasks)
	}
	if len(state.Threads) != 1 || state.Threads[0].Mode != team.InteractionModePanel {
		t.Fatalf("expected planned panel conversation thread, got %#v", state.Threads)
	}
}
