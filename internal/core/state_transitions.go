package core

import (
	"context"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
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

func transitionRunPure(run model.Run, to model.RunStatus) (model.Run, error) {
	return tasksvc.PureRunTransition(run, to)
}

func transitionTaskPure(task model.Task, to model.TaskStatus, bumpVersion bool) (model.Task, error) {
	return tasksvc.PureTaskTransition(task, to, bumpVersion)
}
