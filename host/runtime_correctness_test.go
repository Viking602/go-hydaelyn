package host

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/blackboard"
	"github.com/Viking602/go-hydaelyn/plugin"
	"github.com/Viking602/go-hydaelyn/scheduler"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
	"github.com/Viking602/go-hydaelyn/tool"
)

type failingGateway struct {
	err error
}

func (g failingGateway) ImportTools(context.Context) ([]tool.Driver, error) {
	return nil, g.err
}

type countingPattern struct {
	calls *int
}

func (p countingPattern) Name() string { return "counting" }

func (p countingPattern) Start(_ context.Context, request team.StartRequest) (team.RunState, error) {
	return team.RunState{ID: request.TeamID, Pattern: p.Name(), Status: team.StatusRunning, Phase: team.PhaseResearch}, nil
}

func (p countingPattern) Advance(_ context.Context, state team.RunState) (team.RunState, error) {
	(*p.calls)++
	return state, nil
}

type resumePattern struct{}

func (resumePattern) Name() string { return "resume-pattern" }

func (resumePattern) Start(_ context.Context, request team.StartRequest) (team.RunState, error) {
	return team.RunState{ID: request.TeamID, Pattern: "resume-pattern", Status: team.StatusRunning, Phase: team.PhaseResearch}, nil
}

func (resumePattern) Advance(_ context.Context, state team.RunState) (team.RunState, error) {
	state.Status = team.StatusCompleted
	state.Phase = team.PhaseComplete
	return state, nil
}

func TestNewWithErrorRejectsInvalidPlugins(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec plugin.Spec
	}{
		{
			name: "provider component type mismatch",
			spec: plugin.Spec{Type: plugin.TypeProvider, Name: "bad-provider", Component: "not-a-provider"},
		},
		{
			name: "scheduler component type mismatch",
			spec: plugin.Spec{Type: plugin.TypeScheduler, Name: "bad-scheduler", Component: "not-a-queue"},
		},
		{
			name: "gateway import failure",
			spec: plugin.Spec{Type: plugin.TypeMCPGateway, Name: "bad-gateway", Component: failingGateway{err: errors.New("import failed")}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewWithError(Config{Plugins: []plugin.Spec{tc.spec}})
			if err == nil {
				t.Fatal("expected plugin initialization error")
			}
		})
	}
}

func TestNewPanicsOnPluginInitializationError(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("expected New() to panic when plugin initialization fails")
		}
	}()
	_ = New(Config{
		Plugins: []plugin.Spec{
			{Type: plugin.TypeScheduler, Name: "bad-scheduler", Component: "not-a-queue"},
		},
	})
}

func TestExecuteViaQueueIgnoresForeignLeasesWithoutBlocking(t *testing.T) {
	queue := scheduler.NewMemoryQueue()
	runner := New(Config{Storage: storage.NewMemoryDriver(), WorkerID: "worker-a"})
	if err := runner.RegisterPlugin(plugin.Spec{Type: plugin.TypeScheduler, Name: "memory-queue", Component: queue}); err != nil {
		t.Fatalf("RegisterPlugin() error = %v", err)
	}
	runner.RegisterProvider("team-fake", teamFakeProvider{})
	runner.RegisterPattern(singleTaskPattern{})
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "team-fake", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "team-fake", Model: "test"})

	current, err := singleTaskPattern{}.Start(context.Background(), team.StartRequest{
		TeamID:            "team-a",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input:             map[string]any{"task": "local task"},
	})
	if err != nil {
		t.Fatalf("singleTaskPattern.Start() error = %v", err)
	}
	current.Normalize()

	foreign, err := singleTaskPattern{}.Start(context.Background(), team.StartRequest{
		TeamID:            "team-b",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input:             map[string]any{"task": "foreign task"},
	})
	if err != nil {
		t.Fatalf("singleTaskPattern.Start() error = %v", err)
	}
	foreign.Normalize()

	if err := queue.Enqueue(context.Background(), scheduler.TaskLease{TeamID: foreign.ID, TaskID: foreign.Tasks[0].ID}); err != nil {
		t.Fatalf("Enqueue(foreign) error = %v", err)
	}

	_, runnableSet := runnableTaskSet(current)
	semByProfile, err := runner.buildProfileSemaphores(current, runnableSet)
	if err != nil {
		t.Fatalf("buildProfileSemaphores() error = %v", err)
	}

	done := make(chan struct{})
	var outcomes <-chan taskOutcome
	var executeErr error
	go func() {
		outcomes, executeErr = runner.executeViaQueue(context.Background(), current, runnableSet, semByProfile)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("executeViaQueue() blocked on foreign lease")
	}

	if executeErr != nil {
		t.Fatalf("executeViaQueue() error = %v", executeErr)
	}
	var items []taskOutcome
	for item := range outcomes {
		items = append(items, item)
	}
	if len(items) != 1 {
		t.Fatalf("expected exactly one local outcome, got %#v", items)
	}
	if items[0].task.ID != current.Tasks[0].ID {
		t.Fatalf("expected local task outcome, got %#v", items[0])
	}
}

func TestStartLeaseHeartbeatStopIsIdempotent(t *testing.T) {
	stop := startLeaseHeartbeat(context.Background(), scheduler.NewMemoryQueue(), scheduler.TaskLease{TaskID: "task-1", TeamID: "team-1"}, localQueueLeaseTTL)
	stop()
	stop()
}

func TestMaxTeamDriveStepsAppliesToSyncAndQueuedPaths(t *testing.T) {
	t.Run("driveTeam uses configured limit", func(t *testing.T) {
		calls := 0
		driver := storage.NewMemoryDriver()
		runner := New(Config{Storage: driver, MaxTeamDriveSteps: 2})
		runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor})
		state := team.RunState{
			ID:      "team-sync-limit",
			Pattern: "counting",
			Status:  team.StatusRunning,
			Phase:   team.PhaseResearch,
			Supervisor: team.AgentInstance{
				ID:          "supervisor",
				Role:        team.RoleSupervisor,
				ProfileName: "supervisor",
			},
		}
		state.Normalize()
		if err := driver.Teams().Save(context.Background(), state); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
		state, _ = driver.Teams().Load(context.Background(), state.ID)

		next, err := runner.driveTeam(context.Background(), countingPattern{calls: &calls}, state)
		if err != nil {
			t.Fatalf("driveTeam() error = %v", err)
		}
		if calls != 2 {
			t.Fatalf("expected 2 advance calls, got %d", calls)
		}
		if next.Status != team.StatusFailed {
			t.Fatalf("expected failed state after configured limit, got %#v", next)
		}
	})

	t.Run("continueQueuedTeam uses configured limit", func(t *testing.T) {
		calls := 0
		driver := storage.NewMemoryDriver()
		runner := New(Config{Storage: driver, MaxTeamDriveSteps: 2})
		runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor})
		state := team.RunState{
			ID:      "team-queue-limit",
			Pattern: "counting",
			Status:  team.StatusRunning,
			Phase:   team.PhaseResearch,
			Supervisor: team.AgentInstance{
				ID:          "supervisor",
				Role:        team.RoleSupervisor,
				ProfileName: "supervisor",
			},
		}
		state.Normalize()
		if err := driver.Teams().Save(context.Background(), state); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
		state, _ = driver.Teams().Load(context.Background(), state.ID)

		_, _, err := runner.continueQueuedTeam(context.Background(), countingPattern{calls: &calls}, state)
		if err != nil {
			t.Fatalf("continueQueuedTeam() error = %v", err)
		}
		if calls != 2 {
			t.Fatalf("expected 2 queued advance calls, got %d", calls)
		}
	})
}

func TestResumeTeamPreservesNonEmptyResultPayloads(t *testing.T) {
	tests := []struct {
		name   string
		result *team.Result
		check  func(t *testing.T, got *team.Result)
	}{
		{
			name:   "error-only result",
			result: &team.Result{Error: "paused with error"},
			check: func(t *testing.T, got *team.Result) {
				if got == nil || got.Error != "paused with error" {
					t.Fatalf("expected error-only result to survive, got %#v", got)
				}
			},
		},
		{
			name:   "structured-only result",
			result: &team.Result{Structured: map[string]any{"status": "pending-review"}},
			check: func(t *testing.T, got *team.Result) {
				if got == nil || got.Structured["status"] != "pending-review" {
					t.Fatalf("expected structured-only result to survive, got %#v", got)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			driver := storage.NewMemoryDriver()
			runner := New(Config{Storage: driver})
			runner.RegisterPattern(resumePattern{})
			runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor})

			state := team.RunState{
				ID:      "team-resume-" + strings.ReplaceAll(tc.name, " ", "-"),
				Pattern: "resume-pattern",
				Status:  team.StatusPaused,
				Phase:   team.PhaseResearch,
				Supervisor: team.AgentInstance{
					ID:          "supervisor",
					Role:        team.RoleSupervisor,
					ProfileName: "supervisor",
				},
				Result: tc.result,
			}
			state.Normalize()
			if err := driver.Teams().Save(context.Background(), state); err != nil {
				t.Fatalf("Save() error = %v", err)
			}

			next, err := runner.resumeTeam(context.Background(), state.ID)
			if err != nil {
				t.Fatalf("resumeTeam() error = %v", err)
			}
			tc.check(t, next.Result)
		})
	}
}

func TestAbortTeamCancelledEventUsesAbortedTaskStatus(t *testing.T) {
	driver := storage.NewMemoryDriver()
	runner := New(Config{Storage: driver})
	state := team.RunState{
		ID:      "team-abort-event-status",
		Pattern: "abort-test",
		Status:  team.StatusRunning,
		Phase:   team.PhaseResearch,
		Tasks: []team.Task{{
			ID:     "task-1",
			Status: team.TaskStatusPending,
		}},
	}
	state.Normalize()
	if err := driver.Teams().Save(context.Background(), state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := runner.AbortTeam(context.Background(), state.ID); err != nil {
		t.Fatalf("AbortTeam() error = %v", err)
	}

	events, err := runner.TeamEvents(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("TeamEvents() error = %v", err)
	}
	for _, event := range events {
		if event.Type != storage.EventTaskCancelled || event.TaskID != "task-1" {
			continue
		}
		if status, _ := event.Payload["taskStatus"].(string); status != string(team.TaskStatusAborted) {
			t.Fatalf("expected aborted task status in cancel event, got %#v", event)
		}
		return
	}
	t.Fatalf("expected cancellation event for task-1, got %#v", events)
}

func TestGuardedSynthesisAcceptsIndirectVerifierDependenciesAndNormalizedDecision(t *testing.T) {
	state := team.RunState{
		ID:      "team-guarded-indirect",
		Pattern: "linear",
		Status:  team.StatusRunning,
		Phase:   team.PhaseResearch,
		Tasks: []team.Task{
			{
				ID:        "verify-1",
				Kind:      team.TaskKindVerify,
				Stage:     team.TaskStageVerify,
				Namespace: "verify.impl-api",
				Status:    team.TaskStatusCompleted,
			},
			{
				ID:        "implement-1",
				Kind:      team.TaskKindResearch,
				Stage:     team.TaskStageImplement,
				DependsOn: []string{"verify-1"},
				Status:    team.TaskStatusCompleted,
			},
			{
				ID:               "task-synthesize",
				Kind:             team.TaskKindSynthesize,
				Stage:            team.TaskStageSynthesize,
				DependsOn:        []string{"implement-1"},
				Reads:            []string{"verify.impl-api"},
				VerifierRequired: true,
				Status:           team.TaskStatusPending,
			},
		},
		Blackboard: &blackboard.State{},
	}
	state.Normalize()
	if _, err := state.Blackboard.UpsertExchangeCAS(blackboard.Exchange{
		Key:       verifierGateExchangeKey,
		Namespace: "verify.impl-api",
		TaskID:    "verify-1",
		Version:   1,
		ValueType: blackboard.ExchangeValueTypeJSON,
		Structured: map[string]any{
			verifierGateDecisionField: "APPROVED",
			verifierGateStatusField:   string(blackboard.VerificationStatusSupported),
		},
		Metadata: map[string]string{
			verifierGateDecisionField: "APPROVED",
			verifierGateStatusField:   string(blackboard.VerificationStatusSupported),
		},
	}); err != nil {
		t.Fatalf("UpsertExchangeCAS() error = %v", err)
	}

	if reason, blocked := synthesisVerifierBlockReason(state, state.Tasks[2]); blocked {
		t.Fatalf("expected indirect normalized verifier decision to pass, got reason=%q", reason)
	}
}
