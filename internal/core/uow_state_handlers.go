package core

import (
	"context"

	commandbus "github.com/Viking602/go-hydaelyn/internal/core/command"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
	tasksvc "github.com/Viking602/go-hydaelyn/internal/task"
)

func registerStateUoWCommandHandlers(runtime *Runtime) {
	commandbus.Register[TransitionRunCommand](runtime.commandBus, transitionRunHandler{})
	commandbus.Register[TransitionTaskCommand](runtime.commandBus, transitionTaskHandler{})
}

type transitionRunHandler struct{}

func (transitionRunHandler) Name() string { return TransitionRunCommand{}.CommandName() }

func (transitionRunHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd TransitionRunCommand) (any, error) {
	next, changed, err := tasksvc.TransitionRun(ctx, uow, cmd.RunID, cmd.To)
	if err != nil || !changed {
		return nil, err
	}
	return next, nil
}

type transitionTaskHandler struct{}

func (transitionTaskHandler) Name() string { return TransitionTaskCommand{}.CommandName() }

func (transitionTaskHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd TransitionTaskCommand) (any, error) {
	return tasksvc.TransitionTask(ctx, uow, cmd.RunID, cmd.TaskID, cmd.To, true)
}

func transitionRunPure(run Run, to RunStatus) (Run, error) {
	return tasksvc.PureRunTransition(run, to)
}

func transitionTaskPure(task Task, to TaskStatus, bumpVersion bool) (Task, error) {
	return tasksvc.PureTaskTransition(task, to, bumpVersion)
}
