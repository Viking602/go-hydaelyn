package core

import (
	"context"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

func (r *Runtime) QueueRun(ctx context.Context, cmd StartRunCommand) (model.Run, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return model.Run{}, err
	}
	started, ok := result.(StartRunResult)
	if !ok {
		return model.Run{}, ErrInvalidCommand
	}
	advanced, err := r.ExecuteCommand(ctx, AdvanceRunCommand{RunID: started.Run.ID})
	if err != nil {
		return model.Run{}, err
	}
	return advanced.(model.Run), nil
}
