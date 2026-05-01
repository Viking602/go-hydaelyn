package core

import "context"

func (r *Runtime) PublishResponse(ctx context.Context, cmd PublishResponseCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}
