package task

import "github.com/Viking602/venat/internal/core/model"

type TransitionRunCommand struct {
	RunID string
	To    model.RunStatus
}

type TransitionTaskCommand struct {
	RunID  string
	TaskID string
	To     model.TaskStatus
}

func (TransitionRunCommand) CommandName() string  { return "run.transition" }
func (TransitionTaskCommand) CommandName() string { return "task.transition" }
