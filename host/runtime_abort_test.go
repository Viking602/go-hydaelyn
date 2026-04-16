package host

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/patterns/deepsearch"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

func TestAbortTeamPersistsAbortedStateAndEvent(t *testing.T) {
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
			"query":      "abort",
			"subqueries": []string{"branch"},
		},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	if err := runtime.AbortTeam(context.Background(), state.ID); err != nil {
		t.Fatalf("AbortTeam() error = %v", err)
	}
	current, err := runtime.GetTeam(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("GetTeam() error = %v", err)
	}
	if current.Status != team.StatusAborted {
		t.Fatalf("expected aborted team state, got %#v", current)
	}
	events, err := runtime.TeamEvents(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("TeamEvents() error = %v", err)
	}
	foundCheckpoint := false
	for _, event := range events {
		if event.Type == storage.EventCheckpointSaved {
			foundCheckpoint = true
		}
	}
	if !foundCheckpoint {
		t.Fatalf("expected checkpoint event on abort, got %#v", events)
	}
}
