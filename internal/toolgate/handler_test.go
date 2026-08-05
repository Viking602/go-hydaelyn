package toolgate_test

import (
	"context"
	"errors"
	"testing"

	commandbus "github.com/Viking602/venat/internal/command"
	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
	"github.com/Viking602/venat/internal/memory"
	"github.com/Viking602/venat/internal/toolgate"
)

func TestInvocationAllowsReadOnlyToolWithoutLease(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	if err := uow.Tasks().SaveTask(ctx, model.Task{ID: "task-1", RunID: "run-1", OwnerAgentID: "agent-1"}); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}

	bus := commandbus.NewBus()
	toolgate.RegisterHandlers(bus, toolgate.HandlerOptions{
		Tool: func(name string) (model.Tool, bool) {
			return model.Tool{Name: name, EffectType: model.ToolEffectReadOnly}, true
		},
	})
	result, err := bus.Execute(ctx, uow, toolgate.Invocation{RunID: "run-1", TaskID: "task-1", ToolName: "lookup", Input: "hello"})
	if err != nil {
		t.Fatalf("Invocation error = %v", err)
	}
	invoked := result.(toolgate.InvocationResult)
	if invoked.ToolName != "lookup" || invoked.Output != "hello" {
		t.Fatalf("Invocation result = %#v", invoked)
	}
}

func TestInvocationRejectsWriteToolWithoutActionTask(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	if err := uow.Tasks().SaveTask(ctx, model.Task{ID: "task-1", RunID: "run-1", OwnerAgentID: "agent-1"}); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}

	bus := commandbus.NewBus()
	toolgate.RegisterHandlers(bus, toolgate.HandlerOptions{
		Tool: func(name string) (model.Tool, bool) {
			return model.Tool{Name: name, EffectType: model.ToolEffectWrite}, true
		},
	})
	_, err = bus.Execute(ctx, uow, toolgate.Invocation{RunID: "run-1", TaskID: "task-1", ToolName: "write"})
	if !errors.Is(err, model.ErrActionTaskRequired) {
		t.Fatalf("Invocation error = %v, want ErrActionTaskRequired", err)
	}
}

func TestAgentInvocationUsesHolderScopedToolDefinition(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	if err := uow.Tasks().SaveTask(ctx, model.Task{ID: "task-1", RunID: "run-1", OwnerAgentID: "agent-a"}); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}

	scoped := map[string]model.Tool{
		"agent-a": {Name: "shared", EffectType: model.ToolEffectReadOnly, RiskLevel: "sensitive"},
		"agent-b": {Name: "shared", EffectType: model.ToolEffectReadOnly, RiskLevel: "benign"},
	}
	var seen []string
	bus := commandbus.NewBus()
	toolgate.RegisterHandlers(bus, toolgate.HandlerOptions{
		Tool: func(string) (model.Tool, bool) {
			return scoped["agent-b"], true
		},
		ScopedTool: func(_ string, _ string, _ model.HolderType, holderID, _ string) (model.Tool, bool) {
			tool, ok := scoped[holderID]
			return tool, ok
		},
		Authorize: func(_ context.Context, _ ports.UnitOfWork, request model.PolicyRequest) (model.PolicyDecision, error) {
			seen = append(seen, request.Tool.RiskLevel)
			return model.PolicyDecision{Effect: model.PolicyEffectAllow}, nil
		},
	})

	for _, holderID := range []string{"agent-a", "agent-b"} {
		if _, err := bus.Execute(ctx, uow, toolgate.Invocation{
			RunID: "run-1", TaskID: "task-1", HolderType: model.HolderAgent,
			HolderID: holderID, ToolName: "shared",
		}); err != nil {
			t.Fatalf("Invocation(%s) error = %v", holderID, err)
		}
	}
	if len(seen) != 2 || seen[0] != "sensitive" || seen[1] != "benign" {
		t.Fatalf("authorization metadata = %#v, want [sensitive benign]", seen)
	}
}

func TestAgentInvocationFailsClosedWithoutScopedDefinition(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	if err := uow.Tasks().SaveTask(ctx, model.Task{ID: "task-1", RunID: "run-1", OwnerAgentID: "agent-a"}); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}

	bus := commandbus.NewBus()
	toolgate.RegisterHandlers(bus, toolgate.HandlerOptions{
		Tool: func(string) (model.Tool, bool) {
			return model.Tool{Name: "shared", EffectType: model.ToolEffectReadOnly}, true
		},
	})
	_, err = bus.Execute(ctx, uow, toolgate.Invocation{
		RunID: "run-1", TaskID: "task-1", HolderType: model.HolderAgent,
		HolderID: "agent-a", ToolName: "shared",
	})
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("agent invocation error = %v, want ErrNotFound", err)
	}
}
