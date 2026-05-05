package toolgate_test

import (
	"context"
	"errors"
	"testing"

	commandbus "github.com/Viking602/go-hydaelyn/internal/command"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/memory"
	"github.com/Viking602/go-hydaelyn/internal/toolgate"
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
