package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Viking602/go-hydaelyn/evaluation"
	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/patterns/deepsearch"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/team"
)

type fakeProvider struct{}

func (fakeProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "fake"}
}

func (fakeProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	last := request.Messages[len(request.Messages)-1]
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: last.Text},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete, Usage: provider.Usage{InputTokens: 5, OutputTokens: 8, TotalTokens: 13}},
	}), nil
}

func main() {
	runtime := host.New(host.Config{})
	runtime.RegisterProvider("fake", fakeProvider{})
	runtime.RegisterPattern(deepsearch.New())
	runtime.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "fake", Model: "test"})
	runtime.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "fake", Model: "test"})

	state, err := runtime.StartTeam(context.Background(), host.StartTeamRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher", "researcher"},
		Input: map[string]any{
			"query":      "evaluation example",
			"subqueries": []string{"architecture", "tooling"},
		},
	})
	if err != nil {
		panic(err)
	}

	events, err := runtime.TeamEvents(context.Background(), state.ID)
	if err != nil {
		panic(err)
	}
	report := evaluation.Evaluate(events)
	payload, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(payload))
}
