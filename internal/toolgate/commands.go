package toolgate

import "github.com/Viking602/venat/internal/core/model"

type Invocation struct {
	RunID       string
	TaskID      string
	LeaseID     string
	HolderType  model.HolderType
	HolderID    string
	TaskVersion int
	ToolName    string
	Input       any
}

type InvocationResult struct {
	ToolName string
	Output   any
}

func (Invocation) CommandName() string { return "tool.invoke" }
