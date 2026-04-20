package main

import (
	"context"
	"fmt"

	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/pattern/collab"
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
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

func main() {
	runner := host.New(host.Config{})
	runner.RegisterProvider("echo", echoProvider{})

	// Deepsearch remains the default/reference pattern; collaboration is additive and opt-in.
	runner.RegisterPattern(deepsearch.New())
	runner.RegisterPattern(collab.New())

	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "echo", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "echo", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "verifier", Role: team.RoleVerifier, Provider: "echo", Model: "test"})

	state, err := runner.StartTeam(context.Background(), host.StartTeamRequest{
		Pattern:           "collab",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher", "researcher", "verifier"},
		Input: map[string]any{
			"query":               "ship collaboration workflow",
			"requireVerification": true,
			"branches": []any{
				map[string]any{"id": "impl-api", "title": "implement API", "input": "draft the API contract"},
				map[string]any{"id": "impl-ui", "title": "review UI flow", "input": "draft the UI interaction flow"},
			},
		},
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(state.Result.Summary)
}
