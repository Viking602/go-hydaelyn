package toolgate

import (
	"context"

	commandbus "github.com/Viking602/venat/internal/command"
	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
	"github.com/Viking602/venat/internal/execution"
)

type ToolLookup func(string) (model.Tool, bool)

type ScopedToolLookup func(string, string, model.HolderType, string, string) (model.Tool, bool)

type Authorizer func(context.Context, ports.UnitOfWork, model.PolicyRequest) (model.PolicyDecision, error)

type TraceRecorder func(context.Context, ports.UnitOfWork, string, string, string, string) error

type HandlerOptions struct {
	Tool        ToolLookup
	ScopedTool  ScopedToolLookup
	Authorize   Authorizer
	RecordTrace TraceRecorder
}

func RegisterHandlers(bus *commandbus.Bus, options HandlerOptions) {
	commandbus.Register[Invocation](bus, handler{options: options})
}

type handler struct{ options HandlerOptions }

func (handler) Name() string { return Invocation{}.CommandName() }

func (h handler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd Invocation) (any, error) {
	var (
		tool model.Tool
		ok   bool
	)
	if h.options.ScopedTool != nil {
		tool, ok = h.options.ScopedTool(cmd.RunID, cmd.TaskID, cmd.HolderType, cmd.HolderID, cmd.ToolName)
	}
	if !ok && (cmd.HolderType == "" || cmd.HolderType == model.HolderComponent) && h.options.Tool != nil {
		tool, ok = h.options.Tool(cmd.ToolName)
	}
	if !ok {
		return nil, model.ErrNotFound
	}
	task, err := uow.Tasks().LoadTask(ctx, cmd.RunID, cmd.TaskID)
	if err != nil {
		return nil, err
	}
	if tool.RequiresActionTask || tool.EffectType == model.ToolEffectWrite || tool.EffectType == model.ToolEffectExternalSideEffect {
		if !task.AllowsAction {
			return nil, model.ErrActionTaskRequired
		}
		if _, _, _, err := execution.ValidateSubmission(ctx, uow, cmd.RunID, cmd.TaskID, cmd.LeaseID, cmd.HolderType, cmd.HolderID, cmd.TaskVersion); err != nil {
			return nil, err
		}
	}
	var decision model.PolicyDecision
	if h.options.Authorize != nil {
		decision, err = h.options.Authorize(ctx, uow, model.PolicyRequest{Operation: model.PolicyOperationToolCall, RunID: cmd.RunID, TaskID: cmd.TaskID, Actor: model.SourceIdentity{Type: model.SourceAgent, ID: task.OwnerAgentID}, Tool: &tool})
		if err != nil {
			return nil, err
		}
	}
	if h.options.RecordTrace != nil {
		if err := h.options.RecordTrace(ctx, uow, cmd.RunID, cmd.TaskID, "tool.call", "tool"); err != nil {
			return nil, err
		}
	}
	return InvocationResult{ToolName: cmd.ToolName, Output: cmd.Input, Decision: decision}, nil
}
