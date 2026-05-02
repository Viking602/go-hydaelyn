package core

import (
	"context"

	tasksvc "github.com/Viking602/go-hydaelyn/internal/task"
)

func (r *Runtime) TransitionRun(ctx context.Context, cmd TransitionRunCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runtime) TransitionTask(ctx context.Context, cmd TransitionTaskCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}

func registerStateUoWCommandHandlers(runtime *Runtime) {
	tasksvc.RegisterHandlers(runtime.commandBus)
}

func transitionRunPure(run Run, to RunStatus) (Run, error) {
	return tasksvc.PureRunTransition(run, to)
}

func transitionTaskPure(task Task, to TaskStatus, bumpVersion bool) (Task, error) {
	return tasksvc.PureTaskTransition(task, to, bumpVersion)
}
