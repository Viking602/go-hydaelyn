package main

import (
	"context"
	"encoding/json"
	"fmt"

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
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
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
		WorkerProfiles:    []string{"researcher"},
		Input:             map[string]any{"query": "durable example"},
	})
	if err != nil {
		panic(err)
	}
	replayed, err := runtime.ReplayTeamState(context.Background(), state.ID)
	if err != nil {
		panic(err)
	}
	payload, _ := json.MarshalIndent(replayed, "", "  ")
	fmt.Println(string(payload))
}
