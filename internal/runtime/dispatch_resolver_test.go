package runtime

import (
	"context"
	"errors"
	"testing"
)

func TestResolveRecipientsAddressKinds(t *testing.T) {
	agents := []AgentProfile{
		{ID: "a1", Role: "monitor", Groups: []string{"alpha"}},
		{ID: "a2", Role: "monitor", Groups: []string{"alpha", "beta"}},
		{ID: "a3", Role: "reviewer", Groups: []string{"beta"}},
	}

	got, err := ResolveRecipients(agents, Address{Kind: AddressKindAgent, AgentID: "a2"})
	if err != nil || len(got) != 1 || got[0] != "a2" {
		t.Fatalf("agent address: got=%v err=%v", got, err)
	}

	got, err = ResolveRecipients(agents, Address{Kind: AddressKindRole, Role: "monitor"})
	if err != nil || len(got) != 2 || got[0] != "a1" || got[1] != "a2" {
		t.Fatalf("role address (stable input order): got=%v err=%v", got, err)
	}

	got, err = ResolveRecipients(agents, Address{Kind: AddressKindGroup, Group: "beta"})
	if err != nil || len(got) != 2 || got[0] != "a2" || got[1] != "a3" {
		t.Fatalf("group address: got=%v err=%v", got, err)
	}
}

func TestResolveRecipientsErrors(t *testing.T) {
	agents := []AgentProfile{{ID: "a1", Role: "x"}}

	if _, err := ResolveRecipients(agents, Address{Kind: AddressKindAgent}); !errors.Is(err, ErrInvalidAddress) {
		t.Fatalf("missing agent ID should be ErrInvalidAddress, got %v", err)
	}
	if _, err := ResolveRecipients(agents, Address{Kind: AddressKindRole, Role: "missing"}); !errors.Is(err, ErrNoRecipients) {
		t.Fatalf("unmatched role should be ErrNoRecipients, got %v", err)
	}
	if _, err := ResolveRecipients(agents, Address{Kind: "bogus", AgentID: "x"}); !errors.Is(err, ErrInvalidAddress) {
		t.Fatalf("unknown kind should be ErrInvalidAddress, got %v", err)
	}
}

func TestDispatchTaskFanOutWritesEnvelopePerRecipient(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	rt.RegisterAgent(AgentProfile{ID: "monitor-a", Role: "monitor"})
	rt.RegisterAgent(AgentProfile{ID: "monitor-b", Role: "monitor"})
	rt.RegisterAgent(AgentProfile{ID: "reviewer-a", Role: "reviewer"})

	run := mustStartRun(t, ctx, rt, "run-fanout")
	task := mustCreateTask(t, ctx, rt, CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "broadcast",
		OwnerAgentID: "monitor-a",
	})

	envs, err := rt.DispatchTaskFanOut(ctx, FanOutDispatchTaskCommand{
		RunID:  run.ID,
		TaskID: task.ID,
		To:     Address{Kind: AddressKindRole, Role: "monitor"},
	})
	if err != nil {
		t.Fatalf("DispatchTaskFanOut() error = %v", err)
	}
	if len(envs) != 2 {
		t.Fatalf("expected 2 envelopes for role monitor, got %d", len(envs))
	}
	seen := map[string]bool{}
	for _, env := range envs {
		seen[env.TargetAgentID] = true
		if env.RunID != run.ID || env.TaskID != task.ID || env.Status != "pending" {
			t.Fatalf("envelope shape unexpected: %#v", env)
		}
	}
	if !seen["monitor-a"] || !seen["monitor-b"] {
		t.Fatalf("expected both monitor recipients, seen=%v", seen)
	}

	after := mustLoadTask(t, ctx, rt, run.ID, task.ID)
	if after.Status != TaskStatusDispatched {
		t.Fatalf("fan-out should transition task to Dispatched, got %s", after.Status)
	}
}

func TestDispatchTaskFanOutNoRecipients(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	rt.RegisterAgent(AgentProfile{ID: "lone", Role: "x"})
	run := mustStartRun(t, ctx, rt, "run-fanout-empty")
	task := mustCreateTask(t, ctx, rt, CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "broadcast",
		OwnerAgentID: "lone",
	})
	_, err := rt.DispatchTaskFanOut(ctx, FanOutDispatchTaskCommand{
		RunID:  run.ID,
		TaskID: task.ID,
		To:     Address{Kind: AddressKindRole, Role: "missing"},
	})
	if !errors.Is(err, ErrNoRecipients) {
		t.Fatalf("expected ErrNoRecipients, got %v", err)
	}
}
