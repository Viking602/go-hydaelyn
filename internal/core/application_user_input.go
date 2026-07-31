package core

import (
	"context"

	userinputsvc "github.com/Viking602/venat/internal/userinput"
)

func (r *Runtime) SubmitUserInput(ctx context.Context, cmd SubmitUserInputCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}

func registerUserInputUoWCommandHandlers(runtime *Runtime) {
	userinputsvc.RegisterHandlers(runtime.commandBus, userinputsvc.HandlerOptions{
		NewID:       runtime.newID,
		Authorize:   runtime.authorizeUoW,
		RecordTrace: runtime.recordEndedTraceUoW,
	})
}
