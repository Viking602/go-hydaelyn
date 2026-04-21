package host

import (
	"context"
	"sync"
	"testing"

	"github.com/Viking602/go-hydaelyn/internal/plugin"
	"github.com/Viking602/go-hydaelyn/pattern/deepsearch"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/scheduler"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

type abortCountingProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *abortCountingProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "abort-counting"}
}

func (p *abortCountingProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	last := request.Messages[len(request.Messages)-1].Text
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: last},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

func (p *abortCountingProvider) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func TestAbortTeamPersistsAbortedStateAndEvent(t *testing.T) {
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
			"query":      "abort",
			"subqueries": []string{"branch"},
		},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	if err := runner.AbortTeam(context.Background(), state.ID); err != nil {
		t.Fatalf("AbortTeam() error = %v", err)
	}
	current, err := runner.GetTeam(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("GetTeam() error = %v", err)
	}
	if current.Status != team.StatusAborted {
		t.Fatalf("expected aborted team state, got %#v", current)
	}
	events, err := runner.TeamEvents(context.Background(), state.ID)
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

func TestAbortTeamMarksActiveTasksAborted(t *testing.T) {
	driver := storage.NewMemoryDriver()
	runner := New(Config{Storage: driver})

	state := team.RunState{
		ID:      "team-abort-active-tasks",
		Pattern: "abort-test",
		Status:  team.StatusRunning,
		Phase:   team.PhaseResearch,
		Tasks: []team.Task{
			{ID: "pending-task", Status: team.TaskStatusPending},
			{ID: "running-task", Status: team.TaskStatusRunning},
			{ID: "completed-task", Status: team.TaskStatusCompleted, CompletedBy: "worker-1", Result: &team.Result{Summary: "done"}},
			{ID: "failed-task", Status: team.TaskStatusFailed, Error: "failed before abort"},
			{ID: "skipped-task", Status: team.TaskStatusSkipped, Error: "skipped before abort"},
		},
	}
	state.Normalize()
	if err := driver.Teams().Save(context.Background(), state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := runner.AbortTeam(context.Background(), state.ID); err != nil {
		t.Fatalf("AbortTeam() error = %v", err)
	}

	current, err := runner.GetTeam(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("GetTeam() error = %v", err)
	}
	if current.Status != team.StatusAborted {
		t.Fatalf("expected aborted team state, got %#v", current)
	}

	statuses := map[string]team.TaskStatus{}
	for _, task := range current.Tasks {
		statuses[task.ID] = task.Status
	}
	if statuses["pending-task"] != team.TaskStatusAborted {
		t.Fatalf("expected pending task to abort, got %s", statuses["pending-task"])
	}
	if statuses["running-task"] != team.TaskStatusAborted {
		t.Fatalf("expected running task to abort, got %s", statuses["running-task"])
	}
	if statuses["completed-task"] != team.TaskStatusCompleted {
		t.Fatalf("expected completed task to remain completed, got %s", statuses["completed-task"])
	}
	if statuses["failed-task"] != team.TaskStatusFailed {
		t.Fatalf("expected failed task to remain failed, got %s", statuses["failed-task"])
	}
	if statuses["skipped-task"] != team.TaskStatusSkipped {
		t.Fatalf("expected skipped task to remain skipped, got %s", statuses["skipped-task"])
	}
}

func TestAbortTeamPreventsQueuedTaskExecution(t *testing.T) {
	driver := storage.NewMemoryDriver()
	queue := scheduler.NewMemoryQueue()
	provider := &abortCountingProvider{}
	newRuntime := func(workerID string) *Runtime {
		runner := New(Config{Storage: driver, WorkerID: workerID})
		if err := runner.RegisterPlugin(plugin.Spec{Type: plugin.TypeScheduler, Name: "memory-queue", Component: queue}); err != nil {
			t.Fatalf("RegisterPlugin() error = %v", err)
		}
		runner.RegisterProvider("abort-counting", provider)
		runner.RegisterPattern(singleTaskPattern{})
		runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "abort-counting", Model: "test"})
		runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "abort-counting", Model: "test"})
		return runner
	}

	coordinator := newRuntime("coordinator")
	worker := newRuntime("worker-a")

	state, err := coordinator.QueueTeam(context.Background(), StartTeamRequest{
		Pattern:           "single-task",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input:             map[string]any{"task": "queued task must not run after abort"},
	})
	if err != nil {
		t.Fatalf("QueueTeam() error = %v", err)
	}
	if len(state.Tasks) != 1 || state.Tasks[0].Status != team.TaskStatusPending {
		t.Fatalf("expected queued pending task, got %#v", state.Tasks)
	}

	if err := coordinator.AbortTeam(context.Background(), state.ID); err != nil {
		t.Fatalf("AbortTeam() error = %v", err)
	}

	processed, err := worker.RunQueueWorker(context.Background(), 1)
	if err != nil {
		t.Fatalf("RunQueueWorker() error = %v", err)
	}
	if processed != 1 {
		t.Fatalf("expected worker to release one queued lease, got %d", processed)
	}
	if provider.Calls() != 0 {
		t.Fatalf("expected aborted queued task to never execute, got %d provider calls", provider.Calls())
	}

	current, err := coordinator.GetTeam(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("GetTeam() error = %v", err)
	}
	if current.Status != team.StatusAborted {
		t.Fatalf("expected aborted team state, got %#v", current)
	}
	if len(current.Tasks) != 1 || current.Tasks[0].Status != team.TaskStatusAborted {
		t.Fatalf("expected queued task to remain aborted, got %#v", current.Tasks)
	}
	if _, ok, err := queue.Acquire(context.Background(), "worker-b", localQueueLeaseTTL); err != nil || ok {
		t.Fatalf("expected queue to be empty after releasing aborted task lease, got ok=%v err=%v", ok, err)
	}
}

func TestMultiAgentCollaboration_CancelsChildrenByFailurePolicy(t *testing.T) {
	runner := New(Config{})
	state := team.RunState{
		ID:      "team-failure-policy",
		Pattern: "collab",
		Status:  team.StatusRunning,
		Phase:   team.PhaseResearch,
		Tasks: []team.Task{
			{ID: "fast-root", Kind: team.TaskKindResearch, AssigneeAgentID: "worker-1", FailurePolicy: team.FailurePolicyFailFast, Status: team.TaskStatusPending},
			{ID: "fast-child", Kind: team.TaskKindResearch, AssigneeAgentID: "worker-1", FailurePolicy: team.FailurePolicyFailFast, DependsOn: []string{"fast-root"}, Status: team.TaskStatusPending},
			{ID: "fast-grandchild", Kind: team.TaskKindResearch, AssigneeAgentID: "worker-1", FailurePolicy: team.FailurePolicyFailFast, DependsOn: []string{"fast-child"}, Status: team.TaskStatusPending},
			{ID: "retry-root", Kind: team.TaskKindResearch, AssigneeAgentID: "worker-1", FailurePolicy: team.FailurePolicyRetry, Status: team.TaskStatusPending},
			{ID: "retry-child", Kind: team.TaskKindResearch, AssigneeAgentID: "worker-1", FailurePolicy: team.FailurePolicyFailFast, DependsOn: []string{"retry-root"}, Status: team.TaskStatusPending},
			{ID: "degrade-root", Kind: team.TaskKindResearch, AssigneeAgentID: "worker-1", FailurePolicy: team.FailurePolicyDegrade, Status: team.TaskStatusPending},
			{ID: "degrade-child", Kind: team.TaskKindResearch, AssigneeAgentID: "worker-1", FailurePolicy: team.FailurePolicyFailFast, DependsOn: []string{"degrade-root"}, Status: team.TaskStatusPending},
			{ID: "optional-root", Kind: team.TaskKindResearch, AssigneeAgentID: "worker-1", FailurePolicy: team.FailurePolicySkipOptional, Status: team.TaskStatusPending},
			{ID: "optional-child", Kind: team.TaskKindResearch, AssigneeAgentID: "worker-1", FailurePolicy: team.FailurePolicySkipOptional, DependsOn: []string{"optional-root"}, Status: team.TaskStatusPending},
			{ID: "unrelated", Kind: team.TaskKindResearch, AssigneeAgentID: "worker-1", FailurePolicy: team.FailurePolicyFailFast, Status: team.TaskStatusPending},
		},
	}
	state.Normalize()

	fastFailure := state.Tasks[0]
	fastFailure.Attempts = 1
	fastFailure.Status = team.TaskStatusFailed
	fastFailure.Error = "fast failure"
	fastFailure.Result = &team.Result{Error: fastFailure.Error}
	updated, applied, published, _, err := runner.applyTaskOutcome(context.Background(), state, 0, fastFailure)
	if err != nil {
		t.Fatalf("applyTaskOutcome() error = %v", err)
	}
	if !applied || published {
		t.Fatalf("expected fail-fast outcome to apply without publish, applied=%v published=%v", applied, published)
	}
	if updated.Tasks[1].Status != team.TaskStatusAborted || updated.Tasks[2].Status != team.TaskStatusAborted {
		t.Fatalf("expected transitive fail-fast dependents to abort, got %#v", updated.Tasks[1:3])
	}
	if updated.Tasks[3].Status != team.TaskStatusPending || updated.Tasks[9].Status != team.TaskStatusPending {
		t.Fatalf("expected unrelated work to remain eligible, got retry=%s unrelated=%s", updated.Tasks[3].Status, updated.Tasks[9].Status)
	}

	retryFailure := updated.Tasks[3]
	retryFailure.Attempts = 1
	retryFailure.Status = team.TaskStatusPending
	retryFailure.Error = "retry later"
	updated, applied, published, _, err = runner.applyTaskOutcome(context.Background(), updated, 3, retryFailure)
	if err != nil {
		t.Fatalf("applyTaskOutcome() error = %v", err)
	}
	if !applied || published {
		t.Fatalf("expected retry outcome to requeue without publish, applied=%v published=%v", applied, published)
	}
	if updated.Tasks[4].Status != team.TaskStatusPending {
		t.Fatalf("expected retry child to stay pending, got %#v", updated.Tasks[4])
	}

	degradeFailure := updated.Tasks[5]
	degradeFailure.Attempts = 1
	degradeFailure.Status = team.TaskStatusFailed
	degradeFailure.Error = "degraded failure"
	degradeFailure.Result = &team.Result{Error: degradeFailure.Error}
	updated, applied, published, _, err = runner.applyTaskOutcome(context.Background(), updated, 5, degradeFailure)
	if err != nil {
		t.Fatalf("applyTaskOutcome() error = %v", err)
	}
	if !applied || published {
		t.Fatalf("expected degrade outcome to apply without publish, applied=%v published=%v", applied, published)
	}
	if updated.Tasks[6].Status != team.TaskStatusPending {
		t.Fatalf("expected degrade child to stay pending before dependency resolution, got %#v", updated.Tasks[6])
	}

	optionalFailure := updated.Tasks[7]
	optionalFailure.Attempts = 1
	optionalFailure.Status = team.TaskStatusSkipped
	optionalFailure.Error = "optional branch skipped"
	optionalFailure.Result = &team.Result{Error: optionalFailure.Error}
	updated, applied, published, _, err = runner.applyTaskOutcome(context.Background(), updated, 7, optionalFailure)
	if err != nil {
		t.Fatalf("applyTaskOutcome() error = %v", err)
	}
	if !applied || published {
		t.Fatalf("expected optional outcome to skip without publish, applied=%v published=%v", applied, published)
	}
	if updated.Tasks[8].Status != team.TaskStatusPending {
		t.Fatalf("expected optional child to stay pending before dependency resolution, got %#v", updated.Tasks[8])
	}

	resolved, changed := updated.ResolveBlockedTasks()
	if !changed {
		t.Fatal("expected blocked dependency resolution to run")
	}
	if resolved.Tasks[4].Status != team.TaskStatusPending {
		t.Fatalf("expected retry child to remain pending while retry root is queued, got %#v", resolved.Tasks[4])
	}
	if resolved.Tasks[6].Status != team.TaskStatusFailed {
		t.Fatalf("expected degrade child to fail after dependency resolution, got %#v", resolved.Tasks[6])
	}
	if resolved.Tasks[8].Status != team.TaskStatusSkipped {
		t.Fatalf("expected optional child to skip after dependency resolution, got %#v", resolved.Tasks[8])
	}
	if resolved.Tasks[9].Status != team.TaskStatusPending {
		t.Fatalf("expected unrelated branch to remain pending, got %#v", resolved.Tasks[9])
	}
}
