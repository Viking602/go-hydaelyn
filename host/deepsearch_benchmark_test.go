package host

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/patterns/deepsearch"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/team"
)

type benchmarkProvider struct{}

func (benchmarkProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "bench"}
}

func (benchmarkProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	last := request.Messages[len(request.Messages)-1]
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: last.Text},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

func BenchmarkDeepsearch(b *testing.B) {
	runner := New(Config{})
	runner.RegisterProvider("bench", benchmarkProvider{})
	runner.RegisterPattern(deepsearch.New())
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "bench", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "bench", Model: "test"})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := runner.StartTeam(context.Background(), StartTeamRequest{
			Pattern:           "deepsearch",
			SupervisorProfile: "supervisor",
			WorkerProfiles:    []string{"researcher", "researcher"},
			Input: map[string]any{
				"query":      "benchmark",
				"subqueries": []string{"a", "b", "c"},
			},
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}
