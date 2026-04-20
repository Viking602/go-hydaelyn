package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Viking602/go-hydaelyn/eval"
	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/pattern/deepsearch"
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
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete, Usage: provider.Usage{InputTokens: 5, OutputTokens: 8, TotalTokens: 13}},
	}), nil
}

func main() {
	runner := host.New(host.Config{})
	runner.RegisterProvider("echo", echoProvider{})
	runner.RegisterPattern(deepsearch.New())
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "echo", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "echo", Model: "test"})

	state, err := runner.StartTeam(context.Background(), host.StartTeamRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher", "researcher"},
		Input: map[string]any{
			"query":      "compare ways to add tool integration",
			"subqueries": []string{"runtime design", "tool integration"},
		},
	})
	if err != nil {
		panic(err)
	}

	events, err := runner.TeamEvents(context.Background(), state.ID)
	if err != nil {
		panic(err)
	}
	report := eval.Evaluate(events)
	payload, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(payload))
}
