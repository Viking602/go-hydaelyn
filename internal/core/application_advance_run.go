package core

import (
	"context"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
	runsvc "github.com/Viking602/go-hydaelyn/internal/run"
)

func (r *Runtime) AdvanceRun(ctx context.Context, cmd AdvanceRunCommand) (model.Run, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return model.Run{}, err
	}
	run, ok := result.(model.Run)
	if !ok {
		return model.Run{}, ErrInvalidCommand
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
