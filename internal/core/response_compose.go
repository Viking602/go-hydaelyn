package core

import (
	"context"

	responsesvc "github.com/Viking602/venat/internal/response"
)

func (r *Runtime) SubmitResponseOutput(ctx context.Context, cmd SubmitResponseOutputCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}

func registerResponseUoWCommandHandlers(runtime *Runtime) {
	responsesvc.RegisterSubmitHandler(runtime.commandBus, responsesvc.HandlerOptions{
		NewID:       runtime.newID,
		Authorize:   runtime.authorizeUoW,
		RecordTrace: runtime.recordEndedTraceUoW,
	})
}
