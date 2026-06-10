package multiagent

import (
	"context"
	"testing"
)

func TestTeam_BuilderBindsRosterAndScheduler(t *testing.T) {
	scheduler := SchedulerFunc(func(context.Context, TeamState) ([]Dispatch, error) {
		return nil, nil
	})
	team := NewTeam("research").
		AddRole(AgentClass{Name: "searcher"}).
		AddRole(AgentClass{Name: "writer"}).
		WithScheduler(scheduler)

	if team.Name != "research" {
		t.Fatalf("Name = %q, want research", team.Name)
	}
	if len(team.Agents) != 2 || team.Agents[0].Name != "searcher" || team.Agents[1].Name != "writer" {
		t.Fatalf("unexpected roster: %+v", team.Agents)
	}
	if team.Scheduler == nil {
		t.Fatal("Scheduler not bound")
	}
}
