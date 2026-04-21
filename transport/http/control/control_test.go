package control

import (
	"context"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/internal/plugin"
	"github.com/Viking602/go-hydaelyn/internal/session"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/pattern/deepsearch"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/scheduler"
	"github.com/Viking602/go-hydaelyn/team"
)

type fakeProvider struct{}

func (fakeProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "fake"}
}

func (fakeProvider) Stream(_ context.Context, _ provider.Request) (provider.Stream, error) {
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "hello"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

func TestAPIPromptAndStreamPrompt(t *testing.T) {
	runner := host.New(host.Config{})
	runner.RegisterProvider("fake", fakeProvider{})
	sess, err := runner.CreateSession(context.Background(), session.CreateParams{Branch: "main"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	api := New(runner)

	response, err := api.Prompt(context.Background(), sess.ID, PromptRequest{
		Provider: "fake",
		Model:    "test",
		Messages: []message.Message{message.NewText(message.RoleUser, "hello")},
	})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	if len(response.Messages) == 0 || response.Messages[len(response.Messages)-1].Text != "hello" {
		t.Fatalf("unexpected prompt response %#v", response)
	}

	events := make([]provider.Event, 0, 2)
	streamed, err := api.StreamPrompt(context.Background(), sess.ID, PromptRequest{
		Provider: "fake",
		Model:    "test",
	}, func(event provider.Event) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamPrompt() error = %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("expected stream events, got %#v", events)
	}
	if len(streamed.Messages) == 0 || streamed.Messages[len(streamed.Messages)-1].Text != "hello" {
		t.Fatalf("unexpected streamed response %#v", streamed)
	}
}

func TestAPITeamAndSchedulerOperations(t *testing.T) {
	runner := host.New(host.Config{})
	queue := scheduler.NewMemoryQueue()
	if err := runner.RegisterPlugin(plugin.Spec{
		Type:      plugin.TypeScheduler,
		Name:      "memory",
		Component: queue,
	}); err != nil {
		t.Fatalf("RegisterPlugin() error = %v", err)
	}
	runner.RegisterProvider("fake", fakeProvider{})
	runner.RegisterPattern(deepsearch.New())
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "fake", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "fake", Model: "test"})
	state, err := runner.StartTeam(context.Background(), host.StartTeamRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input:             map[string]any{"query": "hello"},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	api := New(runner)

	items, err := api.ListTeams(context.Background())
	if err != nil || len(items) == 0 {
		t.Fatalf("ListTeams() items=%#v err=%v", items, err)
	}
	current, err := api.GetTeam(context.Background(), state.ID)
	if err != nil || current.ID != state.ID {
		t.Fatalf("GetTeam() current=%#v err=%v", current, err)
	}
	replayed, err := api.ReplayTeam(context.Background(), state.ID)
	if err != nil || replayed.ID != state.ID {
		t.Fatalf("ReplayTeam() current=%#v err=%v", replayed, err)
	}
	events, err := api.TeamEvents(context.Background(), state.ID)
	if err != nil || len(events) == 0 {
		t.Fatalf("TeamEvents() events=%#v err=%v", events, err)
	}
	if err := api.AbortTeam(context.Background(), state.ID); err != nil {
		t.Fatalf("AbortTeam() error = %v", err)
	}

	_ = queue.Enqueue(context.Background(), scheduler.TaskLease{
		TaskID:    "task-x",
		TeamID:    state.ID,
		OwnerID:   "worker",
		ExpiresAt: time.Now().Add(-time.Minute),
	})
	if _, err := api.RecoverScheduler(context.Background(), RecoverSchedulerRequest{}); err != nil {
		t.Fatalf("RecoverScheduler() error = %v", err)
	}
	if _, err := api.DrainScheduler(context.Background(), DrainSchedulerRequest{MaxTasks: 100}); err != nil {
		t.Fatalf("DrainScheduler() error = %v", err)
	}
}
