package core

import "context"

func (r *Runtime) AckEnvelope(ctx context.Context, cmd AckEnvelopeCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runtime) DeadLetter(ctx context.Context, cmd DeadLetterCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}
