package core

import (
	"context"

	"github.com/Viking602/venat/api"
	tasksvc "github.com/Viking602/venat/internal/task"
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

func transitionRunPure(run api.Run, to api.RunStatus) (api.Run, error) {
	return tasksvc.PureRunTransition(run, to)
}

func transitionTaskPure(task api.Task, to api.TaskStatus, bumpVersion bool) (api.Task, error) {
	return tasksvc.PureTaskTransition(task, to, bumpVersion)
}
