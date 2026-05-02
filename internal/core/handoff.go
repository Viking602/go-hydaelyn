package core

import (
	"context"

	handoffsvc "github.com/Viking602/go-hydaelyn/internal/handoff"
)

type HandoffCommand = handoffsvc.HandoffCommand

func (r *Runtime) RequestHandoff(ctx context.Context, cmd HandoffCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}

func registerHandoffUoWCommandHandlers(runtime *Runtime) {
	handoffsvc.RegisterHandlers(runtime.commandBus, handoffsvc.HandlerOptions{
		NewID:       runtime.newID,
		Authorize:   runtime.authorizeUoW,
		RecordTrace: runtime.recordEndedTraceUoW,
		MaxDepth:    maxHandoffDepth,
	})
}
