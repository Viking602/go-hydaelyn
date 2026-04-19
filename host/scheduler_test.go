package host

import (
	"testing"

	"github.com/Viking602/go-hydaelyn/planner"
	"github.com/Viking602/go-hydaelyn/team"
)

func TestSelectAgentForPlanTaskHonorsBudget(t *testing.T) {
	runner := New(Config{})
	runner.RegisterProfile(team.Profile{
		Name:          "small",
		Role:          team.RoleResearcher,
		DefaultBudget: team.Budget{Tokens: 20},
		ToolNames:     []string{"search"},
	})
	runner.RegisterProfile(team.Profile{
		Name:          "large",
		Role:          team.RoleResearcher,
		DefaultBudget: team.Budget{Tokens: 100},
		ToolNames:     []string{"search"},
	})
	workers := []team.AgentInstance{
		{ID: "worker-1", Role: team.RoleResearcher, ProfileName: "small"},
		{ID: "worker-2", Role: team.RoleResearcher, ProfileName: "large"},
	}
	assignee, err := runner.selectAgentForPlanTask(planner.TaskSpec{
		ID:                   "task-1",
		RequiredRole:         team.RoleResearcher,
		RequiredCapabilities: []string{"search"},
		Budget:               team.Budget{Tokens: 50},
	}, workers, nil)
	if err != nil {
		t.Fatalf("selectAgentForPlanTask() error = %v", err)
	}
	if assignee != "worker-2" {
		t.Fatalf("expected worker-2, got %q", assignee)
	}
}

func TestSelectAgentForPlanTaskAvoidsConcurrencySaturation(t *testing.T) {
	runner := New(Config{})
	runner.RegisterProfile(team.Profile{
		Name:           "tight",
		Role:           team.RoleResearcher,
		DefaultBudget:  team.Budget{Tokens: 100},
		ToolNames:      []string{"search"},
		MaxConcurrency: 1,
	})
	runner.RegisterProfile(team.Profile{
		Name:           "wide",
		Role:           team.RoleResearcher,
		DefaultBudget:  team.Budget{Tokens: 100},
		ToolNames:      []string{"search"},
		MaxConcurrency: 2,
	})
	workers := []team.AgentInstance{
		{ID: "worker-1", Role: team.RoleResearcher, ProfileName: "tight"},
		{ID: "worker-2", Role: team.RoleResearcher, ProfileName: "wide"},
	}
	assignments := map[string]int{
		"worker-1": 1,
		"worker-2": 1,
	}
	assignee, err := runner.selectAgentForPlanTask(planner.TaskSpec{
		ID:                   "task-2",
		RequiredRole:         team.RoleResearcher,
		RequiredCapabilities: []string{"search"},
		Budget:               team.Budget{Tokens: 10},
	}, workers, assignments)
	if err != nil {
		t.Fatalf("selectAgentForPlanTask() error = %v", err)
	}
	if assignee != "worker-2" {
		t.Fatalf("expected worker-2 after worker-1 reached concurrency cap, got %q", assignee)
	}
}
