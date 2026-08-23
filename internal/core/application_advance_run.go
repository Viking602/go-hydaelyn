package core

import (
	"context"

	"github.com/Viking602/venat/api"
	runsvc "github.com/Viking602/venat/internal/run"
)

func (r *Runtime) AdvanceRun(ctx context.Context, cmd AdvanceRunCommand) (api.Run, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return api.Run{}, err
	}
	run, ok := result.(api.Run)
	if !ok {
		return api.Run{}, ErrInvalidCommand
	}
	return run, nil
}

type advanceRunResult = runsvc.AdvanceResult

func registerAdvanceRunUoWCommandHandlers(runtime *Runtime) {
	runsvc.RegisterAdvanceHandler(runtime.commandBus, runsvc.AdvanceHandlerOptions{
		NewID:     runtime.newID,
		Pipeline:  runtime.currentPipeline,
		Authorize: runtime.authorizeUoW,
	})
}
