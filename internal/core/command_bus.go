package core

import "context"

// ExecuteCommand routes command through the UoW command bus.
// PublishResponseCommand is the one deliberate exception: response
// publication is a three-phase flow (prepare+commit, gateway call made
// without holding the runtime lock, finalize in a fresh UoW) and so
// cannot run inside a single UoW handler — see Runtime.PublishResponse.
func (r *Runtime) ExecuteCommand(ctx context.Context, command RuntimeCommand) (any, error) {
	if cmd, ok := command.(PublishResponseCommand); ok {
		return nil, r.PublishResponse(ctx, cmd)
	}
	if r.commandBus == nil || !r.commandBus.HasHandler(command.CommandName()) {
		return nil, ErrInvalidCommand
	}
	return r.executeUoWCommand(ctx, command)
}
