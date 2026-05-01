package core

import (
	"context"

	commandbus "github.com/Viking602/go-hydaelyn/internal/core/command"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func registerToolUoWCommandHandlers(runtime *Runtime) {
	commandbus.Register[ToolInvocation](runtime.commandBus, toolInvocationHandler{runtime: runtime})
}

type toolInvocationHandler struct{ runtime *Runtime }

func (toolInvocationHandler) Name() string { return ToolInvocation{}.CommandName() }

func (h toolInvocationHandler) Handle(ctx context.Context, uow ports.FullUnitOfWork, cmd ToolInvocation) (any, error) {
	tool, ok := h.runtime.tool(cmd.ToolName)
	if !ok {
		return nil, ErrNotFound
	}
	task, err := uow.Tasks().LoadTask(ctx, cmd.RunID, cmd.TaskID)
	if err != nil {
		return nil, err
	}
	if tool.RequiresActionTask || tool.EffectType == ToolEffectWrite || tool.EffectType == ToolEffectExternalSideEffect {
		if !task.AllowsAction {
			return nil, ErrActionTaskRequired
		}
		if _, _, _, err := validateSubmissionUoW(ctx, uow, cmd.RunID, cmd.TaskID, cmd.LeaseID, cmd.HolderType, cmd.HolderID, cmd.TaskVersion); err != nil {
			return nil, err
		}
	}
	if _, err := h.runtime.authorizeUoW(ctx, uow, PolicyRequest{Operation: PolicyOperationToolCall, RunID: cmd.RunID, TaskID: cmd.TaskID, Actor: SourceIdentity{Type: SourceAgent, ID: task.OwnerAgentID}, Tool: &tool}); err != nil {
		return nil, err
	}
	if err := h.runtime.recordEndedTraceUoW(ctx, uow, cmd.RunID, cmd.TaskID, "tool.call", "tool"); err != nil {
		return nil, err
	}
	return ToolInvocationResult{ToolName: cmd.ToolName, Output: cmd.Input}, nil
}
