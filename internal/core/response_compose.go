package core

import "context"

func (r *Runtime) SubmitResponseOutput(ctx context.Context, cmd SubmitResponseOutputCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}
