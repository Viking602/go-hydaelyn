package core

import "context"

func (r *Runtime) QueueRun(ctx context.Context, cmd StartRunCommand) (Run, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return Run{}, err
	}
	run := result.([]any)[0].(Run)
	result, err = r.ExecuteCommand(ctx, AdvanceRunCommand{RunID: run.ID})
	if err != nil {
		return Run{}, err
	}
	return result.(Run), nil
}
