package host

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/patterns/deepsearch"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/team"
)

type teamFakeProvider struct{}

func (teamFakeProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "team-fake"}
}

func (teamFakeProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	last := request.Messages[len(request.Messages)-1]
	text := last.Text
	if strings.Contains(text, "slow") {
		time.Sleep(30 * time.Millisecond)
	}
	if strings.Contains(text, "fast") {
		time.Sleep(5 * time.Millisecond)
	}
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: text},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

func TestRuntimeStartTeamRunsParallelWorkersAndAggregatesInTaskOrder(t *testing.T) {
	runtime := New(Config{})
	runtime.RegisterProvider("team-fake", teamFakeProvider{})
	runtime.RegisterPattern(deepsearch.New())
	runtime.RegisterProfile(team.Profile{
		Name:     "supervisor",
		Role:     team.RoleSupervisor,
		Provider: "team-fake",
		Model:    "test",
	})
	runtime.RegisterProfile(team.Profile{
		Name:     "research-a",
		Role:     team.RoleResearcher,
		Provider: "team-fake",
		Model:    "test",
	})
	runtime.RegisterProfile(team.Profile{
		Name:     "research-b",
		Role:     team.RoleResearcher,
		Provider: "team-fake",
		Model:    "test",
	})

	state, err := runtime.StartTeam(context.Background(), StartTeamRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"research-a", "research-b"},
		Input: map[string]any{
			"query":      "parallel search",
			"subqueries": []string{"slow research branch", "fast research branch"},
		},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	if state.Status != team.StatusCompleted {
		t.Fatalf("expected completed team run, got %s", state.Status)
	}
	if state.Result == nil || len(state.Result.Findings) != 2 {
		t.Fatalf("expected aggregated findings, got %#v", state.Result)
	}
	if state.Result.Findings[0].Summary != "slow research branch" {
		t.Fatalf("expected task-order-stable aggregation, got %#v", state.Result.Findings)
	}
	if state.SessionID == "" {
		t.Fatalf("expected team session id")
	}
	for _, task := range state.Tasks {
		if task.AssigneeAgentID == "" {
			t.Fatalf("expected assignee agent id on task %#v", task)
		}
		if task.SessionID == "" {
			t.Fatalf("expected worker session id on task %#v", task)
		}
		if task.SessionID == state.SessionID {
			t.Fatalf("worker session must be isolated from team session: %#v", task)
		}
		snapshot, err := runtime.GetSession(context.Background(), task.SessionID)
		if err != nil {
			t.Fatalf("GetSession(%q) error = %v", task.SessionID, err)
		}
		if snapshot.Session.Scope != message.VisibilityPrivate {
			t.Fatalf("expected private worker session, got %#v", snapshot.Session)
		}
		if snapshot.Session.AgentID != task.AssigneeAgentID {
			t.Fatalf("expected session agent id %q, got %#v", task.AssigneeAgentID, snapshot.Session)
		}
	}
}

func TestRuntimeStartTeamKeepsDistinctAgentInstancesForSameProfileWorkers(t *testing.T) {
	runtime := New(Config{})
	runtime.RegisterProvider("team-fake", teamFakeProvider{})
	runtime.RegisterPattern(deepsearch.New())
	runtime.RegisterProfile(team.Profile{
		Name:     "supervisor",
		Role:     team.RoleSupervisor,
		Provider: "team-fake",
		Model:    "test",
	})
	runtime.RegisterProfile(team.Profile{
		Name:     "researcher",
		Role:     team.RoleResearcher,
		Provider: "team-fake",
		Model:    "test",
	})

	state, err := runtime.StartTeam(context.Background(), StartTeamRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher", "researcher"},
		Input: map[string]any{
			"query":      "parallel search",
			"subqueries": []string{"branch one", "branch two"},
		},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	if len(state.Workers) != 2 {
		t.Fatalf("expected 2 worker instances, got %#v", state.Workers)
	}
	if state.Workers[0].ID == state.Workers[1].ID {
		t.Fatalf("expected distinct worker ids, got %#v", state.Workers)
	}
	if state.Workers[0].EffectiveProfileName() != "researcher" || state.Workers[1].EffectiveProfileName() != "researcher" {
		t.Fatalf("expected shared profile template, got %#v", state.Workers)
	}
	if len(state.Tasks) != 3 {
		t.Fatalf("expected 2 research tasks plus synth task, got %#v", state.Tasks)
	}
	if state.Tasks[0].AssigneeAgentID == state.Tasks[1].AssigneeAgentID {
		t.Fatalf("expected different assignee agent ids, got %#v", state.Tasks)
	}
	if state.Tasks[2].Kind != team.TaskKindSynthesize || state.Tasks[2].AssigneeAgentID != "supervisor" {
		t.Fatalf("expected synth task assigned to supervisor, got %#v", state.Tasks[2])
	}
}
