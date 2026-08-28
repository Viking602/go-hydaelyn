package core

import (
	"context"

	"github.com/Viking602/venat/api"
	responsesvc "github.com/Viking602/venat/internal/response"
)

func (r *Runtime) SubmitResponseOutput(ctx context.Context, cmd SubmitResponseOutputCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}

// ReconcileResponsePublication settles a message the runtime left in
// UserMessagePublishing after an interruption, applying the host's
// determination of whether the gateway actually delivered it.
func (r *Runtime) ReconcileResponsePublication(ctx context.Context, cmd ReconcileResponsePublicationCommand) (api.UserMessage, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return api.UserMessage{}, err
	}
	message, ok := result.(api.UserMessage)
	if !ok {
		return api.UserMessage{}, ErrInvalidCommand
	}
	return message, nil
}

func registerResponseUoWCommandHandlers(runtime *Runtime) {
	options := responsesvc.HandlerOptions{
		NewID:              runtime.newID,
		Authorize:          runtime.authorizeUoW,
		EnforceObligations: runtime.enforceResponseUoW,
		RecordTrace:        runtime.recordEndedTraceUoW,
	}
	responsesvc.RegisterSubmitHandler(runtime.commandBus, options)
	responsesvc.RegisterReconcileHandler(runtime.commandBus, options)
}
