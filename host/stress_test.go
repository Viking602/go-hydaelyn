package host

import (
	"context"
	"fmt"
	"runtime"
	"runtime/debug"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/internal/compact"
	"github.com/Viking602/go-hydaelyn/evaluation"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/session"
	"github.com/Viking602/go-hydaelyn/team"
)

func TestLargeDAG(t *testing.T) {
	runner := New(Config{})
	runner.RegisterProvider("stress", &stressProvider{latency: time.Millisecond})
	runner.RegisterPattern(stressLargeDAGPattern{taskCount: 128})
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "stress", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "stress", Model: "test"})

	state, err := runner.StartTeam(context.Background(), StartTeamRequest{
		Pattern:           "stress-large-dag",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher", "researcher", "researcher", "researcher"},
		Input:             map[string]any{"taskCount": 128},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	if state.Status != team.StatusCompleted {
		t.Fatalf("expected completed state, got %s", state.Status)
	}
	if len(state.Tasks) < 129 {
		t.Fatalf("expected 100+ tasks plus synthesis, got %d", len(state.Tasks))
	}
	for _, task := range state.Tasks {
		if task.Status != team.TaskStatusCompleted {
			t.Fatalf("expected all tasks completed, got %#v", task)
		}
	}
}

func TestBudgetPressure(t *testing.T) {
	runner := New(Config{})
	runner.RegisterProvider("pressure", &stressProvider{usageTokens: 64})
	runner.RegisterPattern(stressBudgetPattern{})
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "pressure", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "pressure", Model: "test"})

	state, err := runner.StartTeam(context.Background(), StartTeamRequest{
		Pattern:           "stress-budget",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher", "researcher"},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	events, err := runner.storage.Events().List(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("Events().List() error = %v", err)
	}
	report := evaluation.Evaluate(events)
	if report.TokenBudgetHitRate != 1 {
		t.Fatalf("expected full token budget pressure, got %#v", report)
	}
	if report.TaskCompletionRate != 1 {
		t.Fatalf("expected completed tasks under pressure, got %#v", report)
	}
	if state.Status != team.StatusCompleted {
		t.Fatalf("expected completed state, got %s", state.Status)
	}
}

func TestLongContextCompaction(t *testing.T) {
	prov := &countingProvider{}
	runner := New(Config{Compactor: &compact.SimpleCompactor{MaxMessages: 12}, CompactThreshold: 12})
	runner.RegisterProvider("counting", prov)

	sess, err := runner.CreateSession(context.Background(), session.CreateParams{})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	for i := 0; i < 200; i++ {
		_, _ = runner.appendSessionMessages(context.Background(), sess.ID, message.NewText(message.RoleUser, fmt.Sprintf("msg-%03d", i)))
	}
	_, err = runner.Prompt(context.Background(), PromptRequest{SessionID: sess.ID, Provider: "counting", Model: "test", Messages: []message.Message{message.NewText(message.RoleUser, "go")}})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	if prov.seenLen != 12 {
		t.Fatalf("expected compacted history of 12 messages, got %d", prov.seenLen)
	}
}

func TestGoroutineLeak(t *testing.T) {
	before := runtime.NumGoroutine()
	for i := 0; i < 12; i++ {
		runner := New(Config{})
		runner.RegisterProvider("stress", &stressProvider{latency: time.Millisecond})
		runner.RegisterPattern(stressBudgetPattern{})
		runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "stress", Model: "test"})
		runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "stress", Model: "test"})
		if _, err := runner.StartTeam(context.Background(), StartTeamRequest{Pattern: "stress-budget", SupervisorProfile: "supervisor", WorkerProfiles: []string{"researcher", "researcher"}}); err != nil {
			t.Fatalf("StartTeam() error = %v", err)
		}
	}
	runtime.GC()
	debug.FreeOSMemory()
	time.Sleep(50 * time.Millisecond)
	after := runtime.NumGoroutine()
	if delta := after - before; delta > 20 {
		t.Fatalf("possible goroutine leak: before=%d after=%d delta=%d", before, after, delta)
	}
}

type stressProvider struct {
	active      int64
	maxObserved int64
	latency     time.Duration
	usageTokens int
}

func (p *stressProvider) Metadata() provider.Metadata { return provider.Metadata{Name: "stress"} }

func (p *stressProvider) Stream(ctx context.Context, request provider.Request) (provider.Stream, error) {
	current := atomic.AddInt64(&p.active, 1)
	defer atomic.AddInt64(&p.active, -1)
	for {
		maxCurrent := atomic.LoadInt64(&p.maxObserved)
		if current <= maxCurrent || atomic.CompareAndSwapInt64(&p.maxObserved, maxCurrent, current) {
			break
		}
	}
	if p.latency > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(p.latency):
		}
	}
	last := request.Messages[len(request.Messages)-1]
	usage := provider.Usage{InputTokens: p.usageTokens / 2, OutputTokens: p.usageTokens / 2, TotalTokens: p.usageTokens}
	return provider.NewSliceStream([]provider.Event{{Kind: provider.EventTextDelta, Text: last.Text}, {Kind: provider.EventDone, StopReason: provider.StopReasonComplete, Usage: usage}}), nil
}

type stressLargeDAGPattern struct{ taskCount int }

func (p stressLargeDAGPattern) Name() string { return "stress-large-dag" }

func (p stressLargeDAGPattern) Start(_ context.Context, request team.StartRequest) (team.RunState, error) {
	workers := make([]team.AgentInstance, 0, len(request.WorkerProfiles))
	for i, name := range request.WorkerProfiles {
		workers = append(workers, team.AgentInstance{ID: fmt.Sprintf("worker-%d", i+1), Role: team.RoleResearcher, ProfileName: name})
	}
	tasks := make([]team.Task, 0, p.taskCount+1)
	deps := make([]string, 0, p.taskCount)
	for i := 0; i < p.taskCount; i++ {
		id := fmt.Sprintf("task-%03d", i+1)
		deps = append(deps, id)
		tasks = append(tasks, team.Task{ID: id, Kind: team.TaskKindResearch, Input: id, RequiredRole: team.RoleResearcher, AssigneeAgentID: workers[i%len(workers)].ID, Status: team.TaskStatusPending, FailurePolicy: team.FailurePolicyFailFast})
	}
	tasks = append(tasks, team.Task{ID: "task-synthesize", Kind: team.TaskKindSynthesize, Input: "synthesize", RequiredRole: team.RoleSupervisor, AssigneeAgentID: "supervisor", DependsOn: deps, Status: team.TaskStatusPending, FailurePolicy: team.FailurePolicyFailFast})
	return team.RunState{ID: request.TeamID, Pattern: p.Name(), Status: team.StatusRunning, Phase: team.PhaseResearch, Supervisor: team.AgentInstance{ID: "supervisor", Role: team.RoleSupervisor, ProfileName: request.SupervisorProfile}, Workers: workers, Tasks: tasks, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, nil
}

func (p stressLargeDAGPattern) Advance(_ context.Context, state team.RunState) (team.RunState, error) {
	allDone := true
	for _, task := range state.Tasks {
		if task.Status == team.TaskStatusPending || task.Status == team.TaskStatusRunning {
			allDone = false
			break
		}
	}
	if allDone {
		state.Status = team.StatusCompleted
		state.Phase = team.PhaseComplete
		state.Result = &team.Result{Summary: "done"}
	}
	return state, nil
}

type stressBudgetPattern struct{}

func (stressBudgetPattern) Name() string { return "stress-budget" }

func (stressBudgetPattern) Start(_ context.Context, request team.StartRequest) (team.RunState, error) {
	workers := []team.AgentInstance{{ID: "worker-1", Role: team.RoleResearcher, ProfileName: request.WorkerProfiles[0]}, {ID: "worker-2", Role: team.RoleResearcher, ProfileName: request.WorkerProfiles[1]}}
	tasks := []team.Task{
		{ID: "task-1", Kind: team.TaskKindResearch, Input: "pressure-a", RequiredRole: team.RoleResearcher, AssigneeAgentID: "worker-1", Status: team.TaskStatusPending, FailurePolicy: team.FailurePolicyFailFast, Budget: team.Budget{Tokens: 8}},
		{ID: "task-2", Kind: team.TaskKindResearch, Input: "pressure-b", RequiredRole: team.RoleResearcher, AssigneeAgentID: "worker-2", Status: team.TaskStatusPending, FailurePolicy: team.FailurePolicyFailFast, Budget: team.Budget{Tokens: 8}},
		{ID: "task-synthesize", Kind: team.TaskKindSynthesize, Input: "synthesize", RequiredRole: team.RoleSupervisor, AssigneeAgentID: "supervisor", DependsOn: []string{"task-1", "task-2"}, Status: team.TaskStatusPending, FailurePolicy: team.FailurePolicyFailFast, Budget: team.Budget{Tokens: 8}},
	}
	return team.RunState{ID: request.TeamID, Pattern: "stress-budget", Status: team.StatusRunning, Phase: team.PhaseResearch, Supervisor: team.AgentInstance{ID: "supervisor", Role: team.RoleSupervisor, ProfileName: request.SupervisorProfile}, Workers: workers, Tasks: tasks, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, nil
}

func (stressBudgetPattern) Advance(_ context.Context, state team.RunState) (team.RunState, error) {
	for _, task := range state.Tasks {
		if task.Status == team.TaskStatusPending || task.Status == team.TaskStatusRunning {
			return state, nil
		}
	}
	state.Status = team.StatusCompleted
	state.Phase = team.PhaseComplete
	state.Result = &team.Result{Summary: "budget pressure handled"}
	return state, nil
}
