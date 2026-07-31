package venat

import (
	"context"
	"testing"

	"github.com/Viking602/venat/api"
)

func TestRegisterAndListAgents(t *testing.T) {
	r := newTestRunner(t)
	r.RegisterAgent(api.AgentProfile{ID: "agent-1", Role: "worker"})
	agents := r.Agents()
	if len(agents) == 0 {
		t.Fatal("expected at least one agent after RegisterAgent")
	}
	if agents[0].ID != "agent-1" {
		t.Errorf("agent ID mismatch: got %q", agents[0].ID)
	}
}

func TestRegisterAgent_IgnoresEmptyID(t *testing.T) {
	r := newTestRunner(t)
	r.RegisterAgent(api.AgentProfile{})
	if len(r.Agents()) != 0 {
		t.Error("agent with empty ID should not be registered")
	}
}

func TestRegisterTool(t *testing.T) {
	r := newTestRunner(t)
	r.RegisterTool(api.Tool{Name: "my-tool"})
}

func TestRegisterFlow_ValidFlow(t *testing.T) {
	r := newTestRunner(t)
	if err := r.RegisterFlow(api.Flow{Name: "my-flow"}); err != nil {
		t.Fatalf("RegisterFlow: %v", err)
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
