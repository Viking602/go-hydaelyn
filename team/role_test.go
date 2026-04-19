package team_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/patterns/deepsearch"
	"github.com/Viking602/go-hydaelyn/planner"
	"github.com/Viking602/go-hydaelyn/plugin"
	"github.com/Viking602/go-hydaelyn/team"
)

type staticPlanner struct{ plan planner.Plan }

func (p staticPlanner) Plan(context.Context, planner.PlanRequest) (planner.Plan, error) {
	return p.plan, nil
}

func (p staticPlanner) Review(context.Context, planner.ReviewInput) (planner.ReviewDecision, error) {
	return planner.ReviewDecision{Action: planner.ReviewActionContinue}, nil
}

func (p staticPlanner) Replan(context.Context, planner.ReplanInput) (planner.Plan, error) {
	return p.plan, nil
}

func TestPlannerRoleValidationRejectsRoleMismatch(t *testing.T) {
	state := team.RunState{
		Supervisor: team.AgentInstance{ID: "supervisor", Role: team.RoleSupervisor, ProfileName: "supervisor"},
		Workers:    []team.AgentInstance{{ID: "worker-1", Role: team.RoleResearcher, ProfileName: "researcher"}},
		Tasks: []team.Task{{
			ID:              "verify-1",
			RequiredRole:    team.RoleVerifier,
			AssigneeAgentID: "worker-1",
			Status:          team.TaskStatusPending,
		}},
	}

	err := state.Validate()
	if err == nil || !strings.Contains(err.Error(), "requires role verifier") {
		t.Fatalf("Validate() error = %v, want role mismatch", err)
	}
}

func TestPlannerCapabilityValidationRejectsMissingCapabilities(t *testing.T) {
	runner := host.New(host.Config{})
	runner.RegisterPattern(deepsearch.New())
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher})

	if err := runner.RegisterPlugin(plugin.Spec{
		Type: plugin.TypePlanner,
		Name: "capability-check",
		Component: staticPlanner{plan: planner.Plan{Tasks: []planner.TaskSpec{{
			ID:                   "task-1",
			Kind:                 string(team.TaskKindResearch),
			Title:                "needs search",
			Input:                "search",
			RequiredRole:         team.RoleResearcher,
			RequiredCapabilities: []string{"search"},
			FailurePolicy:        team.FailurePolicyFailFast,
		}}},
		},
	}); err != nil {
		t.Fatalf("RegisterPlugin() error = %v", err)
	}

	_, err := runner.StartTeam(context.Background(), host.StartTeamRequest{
		Pattern:           "deepsearch",
		Planner:           "capability-check",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input:             map[string]any{"query": "validate capability assignment"},
	})
	if err == nil || !strings.Contains(err.Error(), "no worker matches role researcher, capabilities [search]") {
		t.Fatalf("StartTeam() error = %v, want capability validation failure", err)
	}
}
