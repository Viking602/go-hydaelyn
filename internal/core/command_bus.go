package core

import "context"

func (r *Runtime) ExecuteCommand(ctx context.Context, command RuntimeCommand) (any, error) {
	if r.commandBus == nil || !r.commandBus.HasHandler(command.CommandName()) {
		return nil, ErrInvalidCommand
	}
	return r.executeUoWCommand(ctx, command)
}
