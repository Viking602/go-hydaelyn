package core

import (
	runsvc "github.com/Viking602/go-hydaelyn/internal/run"
	tasksvc "github.com/Viking602/go-hydaelyn/internal/task"
)

type (
	TransitionRunCommand  = tasksvc.TransitionRunCommand
	TransitionTaskCommand = tasksvc.TransitionTaskCommand
	AdvanceRunCommand     = runsvc.AdvanceRunCommand
)
