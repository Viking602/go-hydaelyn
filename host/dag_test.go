package host

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/team"
)

type orderedProvider struct {
	mu                  sync.Mutex
	rootStarted         chan struct{}
	allowRootCompletion chan struct{}
	leafStartedTooEarly bool
	recordedTaskInputs  []string
}

func newOrderedProvider() *orderedProvider {
	return &orderedProvider{
		rootStarted:         make(chan struct{}),
		allowRootCompletion: make(chan struct{}),
	}
}

func (p *orderedProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "ordered"}
}

func (p *orderedProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	last := request.Messages[len(request.Messages)-1].Text
	p.mu.Lock()
	p.recordedTaskInputs = append(p.recordedTaskInputs, last)
	p.mu.Unlock()
	switch last {
	case "root":
		select {
		case <-p.rootStarted:
		default:
			close(p.rootStarted)
		}
		<-p.allowRootCompletion
	case "leaf":
		select {
		case <-p.allowRootCompletion:
		default:
			p.mu.Lock()
			p.leafStartedTooEarly = true
			p.mu.Unlock()
		}
	}
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: last},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

type failingProvider struct {
	mu    sync.Mutex
	calls []string
}

func (p *failingProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "failing"}
}

func (p *failingProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	last := request.Messages[len(request.Messages)-1].Text
	p.mu.Lock()
	p.calls = append(p.calls, last)
	p.mu.Unlock()
	if strings.Contains(last, "fail") {
		return provider.NewSliceStream([]provider.Event{
			{Kind: provider.EventError, Err: errors.New("forced failure")},
		}), nil
	}
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: last},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

type linearPattern struct{}

func (linearPattern) Name() string {
	return "linear"
}

func (linearPattern) Start(_ context.Context, request team.StartRequest) (team.RunState, error) {
	root, _ := request.Input["root"].(string)
	leaf, _ := request.Input["leaf"].(string)
	return team.RunState{
		ID:      request.TeamID,
		Pattern: "linear",
		Status:  team.StatusRunning,
		Phase:   team.PhaseResearch,
		Supervisor: team.AgentInstance{
			ID:          "supervisor",
			Role:        team.RoleSupervisor,
			ProfileName: request.SupervisorProfile,
		},
		Workers: []team.AgentInstance{
			{
				ID:          "worker-1",
				Role:        team.RoleResearcher,
				ProfileName: request.WorkerProfiles[0],
			},
		},
		Tasks: []team.Task{
			{
				ID:              "root",
				Kind:            team.TaskKindResearch,
				Input:           root,
				RequiredRole:    team.RoleResearcher,
				AssigneeAgentID: "worker-1",
				FailurePolicy:   team.FailurePolicyFailFast,
				Status:          team.TaskStatusPending,
			},
			{
				ID:              "leaf",
				Kind:            team.TaskKindResearch,
				Input:           leaf,
				RequiredRole:    team.RoleResearcher,
				AssigneeAgentID: "worker-1",
				FailurePolicy:   team.FailurePolicyFailFast,
				DependsOn:       []string{"root"},
				Status:          team.TaskStatusPending,
			},
		},
		Input: request.Input,
	}, nil
}

func (linearPattern) Advance(_ context.Context, state team.RunState) (team.RunState, error) {
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

func TestExecutesOnlyRunnableTasks(t *testing.T) {
	prov := newOrderedProvider()
	go func() {
		<-prov.rootStarted
		time.Sleep(20 * time.Millisecond)
		close(prov.allowRootCompletion)
	}()

	runner := New(Config{})
	runner.RegisterProvider("ordered", prov)
	runner.RegisterPattern(linearPattern{})
	runner.RegisterProfile(team.Profile{
		Name:     "supervisor",
		Role:     team.RoleSupervisor,
		Provider: "ordered",
		Model:    "test",
	})
	runner.RegisterProfile(team.Profile{
		Name:     "worker",
		Role:     team.RoleResearcher,
		Provider: "ordered",
		Model:    "test",
	})

	state, err := runner.StartTeam(context.Background(), StartTeamRequest{
		Pattern:           "linear",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"worker"},
		Input: map[string]any{
			"root": "root",
			"leaf": "leaf",
		},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	if state.Status != team.StatusCompleted {
		t.Fatalf("expected completed team, got %#v", state)
	}
	prov.mu.Lock()
	defer prov.mu.Unlock()
	if prov.leafStartedTooEarly {
		t.Fatalf("leaf task started before root dependency completed: %#v", prov.recordedTaskInputs)
	}
}

func TestDoesNotExecuteFailedDependenciesDownstreamTasks(t *testing.T) {
	prov := &failingProvider{}
	runner := New(Config{})
	runner.RegisterProvider("failing", prov)
	runner.RegisterPattern(linearPattern{})
	runner.RegisterProfile(team.Profile{
		Name:     "supervisor",
		Role:     team.RoleSupervisor,
		Provider: "failing",
		Model:    "test",
	})
	runner.RegisterProfile(team.Profile{
		Name:     "worker",
		Role:     team.RoleResearcher,
		Provider: "failing",
		Model:    "test",
	})

	state, err := runner.StartTeam(context.Background(), StartTeamRequest{
		Pattern:           "linear",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"worker"},
		Input: map[string]any{
			"root": "fail root",
			"leaf": "leaf",
		},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	if state.Status != team.StatusFailed {
		t.Fatalf("expected failed team, got %#v", state)
	}
	prov.mu.Lock()
	defer prov.mu.Unlock()
	if len(prov.calls) != 1 || prov.calls[0] != "fail root" {
		t.Fatalf("expected only root task execution, got %#v", prov.calls)
	}
}
