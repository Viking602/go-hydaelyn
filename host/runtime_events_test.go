package host

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/patterns/deepsearch"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

func TestRuntimeRecordsEventsAndCanReplayTeamState(t *testing.T) {
	runtime := New(Config{})
	runtime.RegisterProvider("team-fake", teamFakeProvider{})
	runtime.RegisterPattern(deepsearch.New())
	runtime.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "team-fake", Model: "test"})
	runtime.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "team-fake", Model: "test"})

	state, err := runtime.StartTeam(context.Background(), StartTeamRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input: map[string]any{
			"query":      "events",
			"subqueries": []string{"branch"},
		},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	events, err := runtime.storage.Events().List(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("expected runtime events")
	}
	has := map[storage.EventType]bool{}
	for _, event := range events {
		has[event.Type] = true
	}
	for _, eventType := range []storage.EventType{
		storage.EventTeamStarted,
		storage.EventTaskScheduled,
		storage.EventTaskStarted,
		storage.EventTaskCompleted,
		storage.EventTeamCompleted,
	} {
		if !has[eventType] {
			t.Fatalf("expected event %s in %#v", eventType, events)
		}
	}
	replayed, err := runtime.ReplayTeamState(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("ReplayTeamState() error = %v", err)
	}
	if replayed.Status != team.StatusCompleted || replayed.Result == nil {
		t.Fatalf("unexpected replayed state %#v", replayed)
	}
}
