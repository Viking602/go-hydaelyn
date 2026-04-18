package host

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/observe"
	"github.com/Viking602/go-hydaelyn/plugin"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/scheduler"
	"github.com/Viking602/go-hydaelyn/patterns/deepsearch"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

type collaborationObservabilityProvider struct{}

func (collaborationObservabilityProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "collaboration-observe"}
}

func (collaborationObservabilityProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	last := request.Messages[len(request.Messages)-1].Text
	text := "done"
	switch last {
	case "verify-pass":
		text = "supported"
	case "synthesize":
		text = "synthesis committed"
	}
	return provider.NewSliceStream([]provider.Event{{Kind: provider.EventTextDelta, Text: text}, {Kind: provider.EventDone, StopReason: provider.StopReasonComplete}}), nil
}

type collaborationObservabilityPattern struct{}

func (collaborationObservabilityPattern) Name() string { return "collaboration-observability" }

func (collaborationObservabilityPattern) Start(_ context.Context, request team.StartRequest) (team.RunState, error) {
	return team.RunState{
		ID:      request.TeamID,
		Pattern: "collaboration-observability",
		Status:  team.StatusRunning,
		Phase:   team.PhaseVerify,
		Supervisor: team.AgentInstance{ID: "supervisor", Role: team.RoleSupervisor, ProfileName: request.SupervisorProfile},
		Workers: []team.AgentInstance{{ID: "worker-1", Role: team.RoleResearcher, ProfileName: request.WorkerProfiles[0]}},
		Tasks: []team.Task{
			{ID: "verify-1", Kind: team.TaskKindVerify, Stage: team.TaskStageVerify, Input: "verify-pass", RequiredRole: team.RoleResearcher, AssigneeAgentID: "worker-1", FailurePolicy: team.FailurePolicyFailFast, Status: team.TaskStatusPending},
			{ID: "synth-1", Kind: team.TaskKindSynthesize, Stage: team.TaskStageSynthesize, Input: "synthesize", RequiredRole: team.RoleResearcher, AssigneeAgentID: "worker-1", FailurePolicy: team.FailurePolicyFailFast, DependsOn: []string{"verify-1"}, Status: team.TaskStatusPending},
		},
		Input: request.Input,
	}, nil
}

func (collaborationObservabilityPattern) Advance(_ context.Context, state team.RunState) (team.RunState, error) {
	for _, task := range state.Tasks {
		if task.Status == team.TaskStatusPending || task.Status == team.TaskStatusRunning {
			return state, nil
		}
	}
	state.Status = team.StatusCompleted
	state.Phase = team.PhaseComplete
	state.Result = &team.Result{Summary: "done"}
	return state, nil
}

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

func TestMultiAgentCollaboration_EmitsLifecycleObservability(t *testing.T) {
	observer := observe.NewMemoryObserver()
	runtime := New(Config{WorkerID: "worker-observe"})
	runtime.UseObserver(observer)
	if err := runtime.RegisterPlugin(plugin.Spec{Type: plugin.TypeScheduler, Name: "memory-queue", Component: scheduler.NewMemoryQueue()}); err != nil {
		t.Fatalf("RegisterPlugin() error = %v", err)
	}
	runtime.RegisterProvider("collaboration-observe", collaborationObservabilityProvider{})
	runtime.RegisterPattern(collaborationObservabilityPattern{})
	runtime.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "collaboration-observe", Model: "test"})
	runtime.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "collaboration-observe", Model: "test"})

	state, err := runtime.QueueTeam(context.Background(), StartTeamRequest{
		Pattern:           "collaboration-observability",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
	})
	if err != nil {
		t.Fatalf("QueueTeam() error = %v", err)
	}
	if _, err := runtime.RunQueueWorker(context.Background(), 4); err != nil {
		t.Fatalf("RunQueueWorker() error = %v", err)
	}
	current, err := runtime.GetTeam(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("GetTeam() error = %v", err)
	}
	if current.Status != team.StatusCompleted {
		t.Fatalf("expected completed team, got %#v", current)
	}
	runtime.recordLeaseExpiredEvent(context.Background(), current.ID, "verify-1", "worker-observe", "heartbeat_expired")
	runtime.recordStaleWriteRejectedEvent(context.Background(), current.ID, "verify-1", "worker-observe", "state_version_conflict")

	events, err := runtime.TeamEvents(context.Background(), current.ID)
	if err != nil {
		t.Fatalf("TeamEvents() error = %v", err)
	}
	required := map[storage.EventType]bool{
		storage.EventLeaseAcquired:      false,
		storage.EventLeaseExpired:       false,
		storage.EventVerifierPassed:     false,
		storage.EventStaleWriteRejected: false,
		storage.EventSynthesisCommitted: false,
	}
	for _, event := range events {
		if _, ok := required[event.Type]; !ok {
			continue
		}
		required[event.Type] = true
		if event.TeamID == "" {
			t.Fatalf("expected team correlation on %#v", event)
		}
		if event.Payload["traceId"] == "" || event.Payload["correlationId"] == "" {
			t.Fatalf("expected trace/correlation IDs on %#v", event)
		}
	}
	for eventType, seen := range required {
		if !seen {
			t.Fatalf("expected collaboration event %s in %#v", eventType, events)
		}
	}
	counters := observer.Counters()
	for _, name := range []string{
		"collaboration_leases_acquired",
		"collaboration_leases_expired",
		"collaboration_verifier_passed",
		"collaboration_stale_writes_rejected",
	} {
		if counters[name] == 0 {
			t.Fatalf("expected counter %s, got %#v", name, counters)
		}
	}
	logs := observer.Logs()
	if len(logs) == 0 {
		t.Fatalf("expected collaboration logs")
	}
}
