package venat

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/venat/api"
)

func TestRegisterAndListAgents(t *testing.T) {
	r := newTestRunner(t)
	if err := r.RegisterAgent(api.AgentProfile{ID: "agent-1", Role: "worker"}); err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	agents := r.Agents()
	if len(agents) == 0 {
		t.Fatal("expected at least one agent after RegisterAgent")
	}
	if agents[0].ID != "agent-1" {
		t.Errorf("agent ID mismatch: got %q", agents[0].ID)
	}
}

func TestRegisterAgent_RejectsEmptyID(t *testing.T) {
	r := newTestRunner(t)
	if err := r.RegisterAgent(api.AgentProfile{}); !errors.Is(err, api.ErrInvalidCommand) {
		t.Fatalf("RegisterAgent(empty) error = %v, want ErrInvalidCommand", err)
	}
	if len(r.Agents()) != 0 {
		t.Error("agent with empty ID should not be registered")
	}
}

func TestRegisterTool(t *testing.T) {
	r := newTestRunner(t)
	if err := r.RegisterTool(api.Tool{Name: "my-tool"}); err != nil {
		t.Fatalf("RegisterTool: %v", err)
	}
}

func TestRegistration_RejectsMissingIdentity(t *testing.T) {
	tests := []struct {
		name     string
		register func(*Runner) error
	}{
		{
			name:     "tool without a name",
			register: func(r *Runner) error { return r.RegisterTool(api.Tool{}) },
		},
		{
			name: "scoped tool without a run",
			register: func(r *Runner) error {
				return r.RegisterToolForInvocation("", "task-1", api.HolderAgent, "agent-1", api.Tool{Name: "t"})
			},
		},
		{
			name: "scoped tool without a name",
			register: func(r *Runner) error {
				return r.RegisterToolForInvocation("run-1", "task-1", api.HolderAgent, "agent-1", api.Tool{})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.register(newTestRunner(t)); !errors.Is(err, api.ErrInvalidCommand) {
				t.Fatalf("register error = %v, want ErrInvalidCommand", err)
			}
		})
	}
}

func TestStoreProvider_NotNil(t *testing.T) {
	r := newTestRunner(t)
	if r.StoreProvider() == nil {
		t.Error("StoreProvider should not be nil")
	}
}

func TestBegin_ReturnsUnitOfWork(t *testing.T) {
	r := newTestRunner(t)
	uow, err := r.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if uow == nil {
		t.Error("expected non-nil UnitOfWork")
	}
}

func TestSaveAndLoadRun(t *testing.T) {
	r := newTestRunner(t)
	ctx := context.Background()
	run, _, _ := r.StartRun(ctx, api.StartRunCommand{Request: "test"})
	run.Metadata = map[string]string{"key": "val"}
	if err := r.SaveRun(ctx, run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	loaded, err := r.LoadRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if loaded.Metadata["key"] != "val" {
		t.Errorf("metadata not persisted: got %v", loaded.Metadata)
	}
}

func TestListRunsFiltersAgentMetadata(t *testing.T) {
	r := newTestRunner(t)
	ctx := context.Background()
	run, _, err := r.StartRun(ctx, api.StartRunCommand{Request: "test"})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	run.Metadata = map[string]string{"agentId": "agent-1", "agentVersion": "v1"}
	if err := r.SaveRun(ctx, run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	uow, err := r.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	got, err := uow.Runs().ListRuns(ctx, api.RunSelector{AgentID: "agent-1", AgentVersion: "v1"})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(got) != 1 || got[0].ID != run.ID {
		t.Fatalf("ListRuns = %#v, want %s", got, run.ID)
	}
	miss, err := uow.Runs().ListRuns(ctx, api.RunSelector{AgentID: "other"})
	if err != nil {
		t.Fatalf("ListRuns(miss): %v", err)
	}
	if len(miss) != 0 {
		t.Fatalf("ListRuns(other) = %#v, want empty", miss)
	}
}

func TestAppendAndListEvents(t *testing.T) {
	r := newTestRunner(t)
	ctx := context.Background()
	run, _, _ := r.StartRun(ctx, api.StartRunCommand{Request: "test"})
	before, _ := r.ListEvents(ctx, run.ID)
	if err := r.AppendEvent(ctx, api.Event{RunID: run.ID, Type: api.EventType("custom.event")}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	after, err := r.ListEvents(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(after) <= len(before) {
		t.Error("event count should increase after AppendEvent")
	}
}
