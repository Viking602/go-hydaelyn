package core

import (
	runsvc "github.com/Viking602/venat/internal/run"
	tasksvc "github.com/Viking602/venat/internal/task"
)

type (
	TransitionRunCommand  = tasksvc.TransitionRunCommand
	TransitionTaskCommand = tasksvc.TransitionTaskCommand
	AdvanceRunCommand     = runsvc.AdvanceRunCommand
)
