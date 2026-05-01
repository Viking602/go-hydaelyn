package core

import "context"

type ToolInvocation struct {
	RunID       string
	TaskID      string
	LeaseID     string
	HolderType  HolderType
	HolderID    string
	TaskVersion int
	ToolName    string
	Input       any
}

type ToolInvocationResult struct {
	ToolName string
	Output   any
}

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
