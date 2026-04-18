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

func TestMultiAgentCollaboration_CancelsChildrenByFailurePolicy(t *testing.T) {
	runtime := New(Config{})
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
	updated, applied, published := runtime.applyTaskOutcome(state, 0, fastFailure)
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
	updated, applied, published = runtime.applyTaskOutcome(updated, 3, retryFailure)
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
	updated, applied, published = runtime.applyTaskOutcome(updated, 5, degradeFailure)
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
	updated, applied, published = runtime.applyTaskOutcome(updated, 7, optionalFailure)
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
