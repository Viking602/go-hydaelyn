package core

import (
	"context"

	"github.com/Viking602/venat/api"
)

func (r *Runtime) QueueRun(ctx context.Context, cmd StartRunCommand) (api.Run, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return api.Run{}, err
	}
	started, ok := result.(StartRunResult)
	if !ok {
		return api.Run{}, ErrInvalidCommand
	}
	advanced, err := r.ExecuteCommand(ctx, AdvanceRunCommand{RunID: started.Run.ID})
	if err != nil {
		return api.Run{}, err
	}
	return advanced.(api.Run), nil
}
