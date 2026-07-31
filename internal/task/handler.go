package task

import (
	"context"

	commandbus "github.com/Viking602/venat/internal/command"
	"github.com/Viking602/venat/internal/core/ports"
)

func RegisterHandlers(bus *commandbus.Bus) {
	commandbus.Register[TransitionRunCommand](bus, transitionRunHandler{})
	commandbus.Register[TransitionTaskCommand](bus, transitionTaskHandler{})
}

type transitionRunHandler struct{}

func (transitionRunHandler) Name() string { return TransitionRunCommand{}.CommandName() }

func (transitionRunHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd TransitionRunCommand) (any, error) {
	next, changed, err := TransitionRun(ctx, uow, cmd.RunID, cmd.To)
	if err != nil || !changed {
		return nil, err
	}
	return next, nil
}

type transitionTaskHandler struct{}

func (transitionTaskHandler) Name() string { return TransitionTaskCommand{}.CommandName() }

func (transitionTaskHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd TransitionTaskCommand) (any, error) {
	return TransitionTask(ctx, uow, cmd.RunID, cmd.TaskID, cmd.To, true)
}
