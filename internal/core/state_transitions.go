package core

import "context"

func (r *Runtime) TransitionRun(ctx context.Context, cmd TransitionRunCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runtime) TransitionTask(ctx context.Context, cmd TransitionTaskCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}
