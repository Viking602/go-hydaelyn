package core

import "context"

func (r *Runtime) SubmitUserInput(ctx context.Context, cmd SubmitUserInputCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}
