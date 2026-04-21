package main

import (
	"context"
	"fmt"

	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/internal/plugin"
	"github.com/Viking602/go-hydaelyn/pattern/deepsearch"
	"github.com/Viking602/go-hydaelyn/planner"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/team"
)

type echoProvider struct{}

func (echoProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "echo"}
}

func (echoProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	last := request.Messages[len(request.Messages)-1]
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: last.Text},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

type askHumanPlanner struct{}

func (askHumanPlanner) Plan(_ context.Context, request planner.PlanRequest) (planner.Plan, error) {
	return planner.Plan{
		Goal: request.Goal,
		Tasks: []planner.TaskSpec{
			{ID: "task-1", Kind: string(team.TaskKindResearch), Title: "branch", Input: "branch", RequiredRole: team.RoleResearcher},
		},
	}, nil
}

func (askHumanPlanner) Review(_ context.Context, _ planner.ReviewInput) (planner.ReviewDecision, error) {
	return planner.ReviewDecision{Action: planner.ReviewActionAskHuman, Reason: "need human approval"}, nil
}

func (askHumanPlanner) Replan(_ context.Context, _ planner.ReplanInput) (planner.Plan, error) {
	return planner.Plan{}, nil
}

func main() {
	runner := host.New(host.Config{})
	runner.RegisterProvider("echo", echoProvider{})
	runner.RegisterPattern(deepsearch.New())
	_ = runner.RegisterPlugin(plugin.Spec{Type: plugin.TypePlanner, Name: "ask-human", Component: askHumanPlanner{}})
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "echo", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "echo", Model: "test"})
	state, err := runner.StartTeam(context.Background(), host.StartTeamRequest{
		Pattern:           "deepsearch",
		Planner:           "ask-human",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input:             map[string]any{"query": "review a rollout plan that needs approval"},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(state.Status)
	fmt.Println(state.Result.Error)
}
