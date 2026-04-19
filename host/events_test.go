package host

import (
	"context"
	"sync"
	"testing"

	"github.com/Viking602/go-hydaelyn/observe"
	"github.com/Viking602/go-hydaelyn/patterns/deepsearch"
	"github.com/Viking602/go-hydaelyn/plugin"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/scheduler"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

type sequenceBarrierDriver struct {
	*storage.MemoryDriver
	events storage.EventStore
}

func (d *sequenceBarrierDriver) Events() storage.EventStore {
	return d.events
}

type sequenceBarrierEventStore struct {
	inner   storage.EventStore
	workers int

	mu       sync.Mutex
	cond     *sync.Cond
	listed   int
	appended int
	round    int
}

func newSequenceBarrierEventStore(inner storage.EventStore, workers int) *sequenceBarrierEventStore {
	store := &sequenceBarrierEventStore{inner: inner, workers: workers}
	store.cond = sync.NewCond(&store.mu)
	return store
}

func (s *sequenceBarrierEventStore) List(ctx context.Context, runID string) ([]storage.Event, error) {
	return s.inner.List(ctx, runID)
}

func (s *sequenceBarrierEventStore) Append(ctx context.Context, event storage.Event) error {
	if err := s.inner.Append(ctx, event); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	round := s.round
	s.appended++
	if s.appended == s.workers {
		s.listed = 0
		s.appended = 0
		s.round++
		s.cond.Broadcast()
	} else {
		for round == s.round && s.appended < s.workers {
			s.cond.Wait()
		}
	}
	return nil
}

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
		ID:         request.TeamID,
		Pattern:    "collaboration-observability",
		Status:     team.StatusRunning,
		Phase:      team.PhaseVerify,
		Supervisor: team.AgentInstance{ID: "supervisor", Role: team.RoleSupervisor, ProfileName: request.SupervisorProfile},
		Workers:    []team.AgentInstance{{ID: "worker-1", Role: team.RoleResearcher, ProfileName: request.WorkerProfiles[0]}},
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

func TestRecordsEventsAndCanReplayTeamState(t *testing.T) {
	runner := New(Config{})
	runner.RegisterProvider("team-fake", teamFakeProvider{})
	runner.RegisterPattern(deepsearch.New())
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "team-fake", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "team-fake", Model: "test"})

	state, err := runner.StartTeam(context.Background(), StartTeamRequest{
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
	events, err := runner.storage.Events().List(context.Background(), state.ID)
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
	replayed, err := runner.ReplayTeamState(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("ReplayTeamState() error = %v", err)
	}
	if replayed.Status != team.StatusCompleted || replayed.Result == nil {
		t.Fatalf("unexpected replayed state %#v", replayed)
	}
}

func TestMultiAgentCollaboration_EmitsLifecycleObservability(t *testing.T) {
	observer := observe.NewMemoryObserver()
	runner := New(Config{WorkerID: "worker-observe"})
	runner.UseObserver(observer)
	if err := runner.RegisterPlugin(plugin.Spec{Type: plugin.TypeScheduler, Name: "memory-queue", Component: scheduler.NewMemoryQueue()}); err != nil {
		t.Fatalf("RegisterPlugin() error = %v", err)
	}
	runner.RegisterProvider("collaboration-observe", collaborationObservabilityProvider{})
	runner.RegisterPattern(collaborationObservabilityPattern{})
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "collaboration-observe", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "collaboration-observe", Model: "test"})

	state, err := runner.QueueTeam(context.Background(), StartTeamRequest{
		Pattern:           "collaboration-observability",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
	})
	if err != nil {
		t.Fatalf("QueueTeam() error = %v", err)
	}
	if _, err := runner.RunQueueWorker(context.Background(), 4); err != nil {
		t.Fatalf("RunQueueWorker() error = %v", err)
	}
	current, err := runner.GetTeam(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("GetTeam() error = %v", err)
	}
	if current.Status != team.StatusCompleted {
		t.Fatalf("expected completed team, got %#v", current)
	}
	runner.recordLeaseExpiredEvent(context.Background(), current.ID, "verify-1", "worker-observe", eventReasonHeartbeatExpired)
	runner.recordStaleWriteRejectedEvent(context.Background(), current.ID, "verify-1", "worker-observe", eventReasonStateVersionConflict)

	events, err := runner.TeamEvents(context.Background(), current.ID)
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

func TestAppendEventConcurrentSequenceUniqueness(t *testing.T) {
	const (
		workers   = 10
		perWorker = 10
	)

	driver := storage.NewMemoryDriver()
	runner := New(Config{Storage: &sequenceBarrierDriver{
		MemoryDriver: driver,
		events:       newSequenceBarrierEventStore(driver.Events(), workers),
	}})

	ctx := context.Background()
	runID := "run-concurrent-sequences"
	start := make(chan struct{})
	errCh := make(chan error, workers)
	var wg sync.WaitGroup

	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			for index := range perWorker {
				if err := runner.appendEvent(ctx, storage.Event{
					RunID: runID,
					Type:  storage.EventTaskStarted,
					Payload: map[string]any{
						"worker": worker,
						"index":  index,
					},
				}); err != nil {
					errCh <- err
					return
				}
			}
		}(worker)
	}

	close(start)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("appendEvent() error = %v", err)
		}
	}

	events, err := runner.storage.Events().List(ctx, runID)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(events) != workers*perWorker {
		t.Fatalf("expected %d events, got %d", workers*perWorker, len(events))
	}

	seen := make(map[int]storage.Event, len(events))
	for i, event := range events {
		if previous, ok := seen[event.Sequence]; ok {
			t.Fatalf("duplicate sequence %d: first=%#v second=%#v", event.Sequence, previous, event)
		}
		seen[event.Sequence] = event
		if event.Sequence != i+1 {
			t.Fatalf("expected append order sequence %d at index %d, got %#v", i+1, i, event)
		}
	}
}
