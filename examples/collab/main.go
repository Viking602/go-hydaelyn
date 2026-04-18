package main

import (
	"context"
	"fmt"

	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/patterns/collab"
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
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

func main() {
	runtime := host.New(host.Config{})
	runtime.RegisterProvider("fake", fakeProvider{})

	// Deepsearch remains the default/reference pattern; collaboration is additive and opt-in.
	runtime.RegisterPattern(deepsearch.New())
	runtime.RegisterPattern(collab.New())

	runtime.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "fake", Model: "test"})
	runtime.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "fake", Model: "test"})
	runtime.RegisterProfile(team.Profile{Name: "verifier", Role: team.RoleVerifier, Provider: "fake", Model: "test"})

	state, err := runtime.StartTeam(context.Background(), host.StartTeamRequest{
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
