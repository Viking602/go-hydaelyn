package core

import (
	"context"

	toolgatesvc "github.com/Viking602/go-hydaelyn/internal/toolgate"
)

type (
	ToolInvocation       = toolgatesvc.Invocation
	ToolInvocationResult = toolgatesvc.InvocationResult
)

func (r *Runtime) InvokeTool(ctx context.Context, cmd ToolInvocation) (ToolInvocationResult, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return ToolInvocationResult{}, err
	}
	toolResult, ok := result.(ToolInvocationResult)
	if !ok {
		return ToolInvocationResult{}, ErrInvalidCommand
	}
	return toolResult, nil
}

func registerToolUoWCommandHandlers(runtime *Runtime) {
	toolgatesvc.RegisterHandlers(runtime.commandBus, toolgatesvc.HandlerOptions{
		Tool:        runtime.tool,
		Authorize:   runtime.authorizeUoW,
		RecordTrace: runtime.recordEndedTraceUoW,
	})
}
