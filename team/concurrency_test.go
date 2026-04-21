package team_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/team"
)

func TestMaxConcurrencySchedulingUnderLoad(t *testing.T) {
	tracker := &concurrencyTracker{}
	runner := host.New(host.Config{})
	runner.RegisterProvider("team-load", &teamLoadProvider{tracker: tracker, latency: 15 * time.Millisecond})
	runner.RegisterPattern(teamLoadPattern{taskCount: 12})
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "team-load", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "team-load", Model: "test", MaxConcurrency: 2})

	state, err := runner.StartTeam(context.Background(), host.StartTeamRequest{
		Pattern:           "team-load",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher", "researcher", "researcher"},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	if state.Status != team.StatusCompleted {
		t.Fatalf("expected completed state, got %s", state.Status)
	}
	if got := tracker.Max(); got > 2 {
		t.Fatalf("expected profile max concurrency to cap execution at 2, got %d", got)
	}
	completed := 0
	for _, task := range state.Tasks {
		if task.Kind == team.TaskKindResearch && task.Status == team.TaskStatusCompleted {
			completed++
		}
	}
	if completed != 12 {
		t.Fatalf("expected all load tasks completed, got %d", completed)
	}
}

type concurrencyTracker struct {
	active int64
	max    int64
}

func (t *concurrencyTracker) Start() {
	current := atomic.AddInt64(&t.active, 1)
	for {
		maxCurrent := atomic.LoadInt64(&t.max)
		if current <= maxCurrent || atomic.CompareAndSwapInt64(&t.max, maxCurrent, current) {
			return
		}
	}
}

func (t *concurrencyTracker) Done()    { atomic.AddInt64(&t.active, -1) }
func (t *concurrencyTracker) Max() int { return int(atomic.LoadInt64(&t.max)) }

type teamLoadProvider struct {
	tracker *concurrencyTracker
	latency time.Duration
}

func (p *teamLoadProvider) Metadata() provider.Metadata { return provider.Metadata{Name: "team-load"} }

func (p *teamLoadProvider) Stream(ctx context.Context, request provider.Request) (provider.Stream, error) {
	p.tracker.Start()
	defer p.tracker.Done()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(p.latency):
	}
	last := request.Messages[len(request.Messages)-1]
	return provider.NewSliceStream([]provider.Event{{Kind: provider.EventTextDelta, Text: last.Text}, {Kind: provider.EventDone, StopReason: provider.StopReasonComplete}}), nil
}

type teamLoadPattern struct{ taskCount int }

func (p teamLoadPattern) Name() string { return "team-load" }

func (p teamLoadPattern) Start(_ context.Context, request team.StartRequest) (team.RunState, error) {
	workers := make([]team.AgentInstance, 0, len(request.WorkerProfiles))
	for i, name := range request.WorkerProfiles {
		workers = append(workers, team.AgentInstance{ID: fmt.Sprintf("worker-%d", i+1), Role: team.RoleResearcher, ProfileName: name})
	}
	tasks := make([]team.Task, 0, p.taskCount+1)
	for i := 0; i < p.taskCount; i++ {
		tasks = append(tasks, team.Task{ID: fmt.Sprintf("task-%02d", i+1), Kind: team.TaskKindResearch, Input: fmt.Sprintf("load-%02d", i+1), RequiredRole: team.RoleResearcher, AssigneeAgentID: workers[i%len(workers)].ID, Status: team.TaskStatusPending, FailurePolicy: team.FailurePolicyFailFast})
	}
	dependsOn := make([]string, 0, p.taskCount)
	for _, task := range tasks {
		dependsOn = append(dependsOn, task.ID)
	}
	tasks = append(tasks, team.Task{ID: "task-synthesize", Kind: team.TaskKindSynthesize, Input: "synthesize", RequiredRole: team.RoleSupervisor, AssigneeAgentID: "supervisor", DependsOn: dependsOn, Status: team.TaskStatusPending, FailurePolicy: team.FailurePolicyFailFast})
	return team.RunState{ID: request.TeamID, Pattern: p.Name(), Status: team.StatusRunning, Phase: team.PhaseResearch, Supervisor: team.AgentInstance{ID: "supervisor", Role: team.RoleSupervisor, ProfileName: request.SupervisorProfile}, Workers: workers, Tasks: tasks, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, nil
}

func (p teamLoadPattern) Advance(_ context.Context, state team.RunState) (team.RunState, error) {
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
