package core

import (
	"context"

	runsvc "github.com/Viking602/go-hydaelyn/internal/run"
)

func (r *Runtime) AdvanceRun(ctx context.Context, cmd AdvanceRunCommand) (Run, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return Run{}, err
	}
	run, ok := result.(Run)
	if !ok {
		return Run{}, ErrInvalidCommand
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
