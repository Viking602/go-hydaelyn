package core

import "context"

func (r *Runtime) QueueRun(ctx context.Context, cmd StartRunCommand) (Run, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return Run{}, err
	}
	started, ok := result.(StartRunResult)
	if !ok {
		return Run{}, ErrInvalidCommand
	}
	advanced, err := r.ExecuteCommand(ctx, AdvanceRunCommand{RunID: started.Run.ID})
	if err != nil {
		return Run{}, err
	}
	return advanced.(Run), nil
}
