package venat

import (
	"context"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core"
)

func (r *Runner) DispatchTask(ctx context.Context, cmd api.DispatchTaskCommand) (api.TaskEnvelope, error) {
	envelope, err := r.rt.DispatchTask(ctx, core.DispatchTaskCommand{
		RunID:           cmd.RunID,
		TaskID:          cmd.TaskID,
		TargetAgentID:   cmd.TargetAgentID,
		TargetComponent: cmd.TargetComponent,
		Payload:         cloneAnyMap(cmd.Payload),
	})
	if err != nil {
		return api.TaskEnvelope{}, err
	}
	return envelope, nil
}

func (r *Runner) DispatchTaskFanOut(ctx context.Context, cmd api.FanOutDispatchTaskCommand) ([]api.TaskEnvelope, error) {
	envelopes, err := r.rt.DispatchTaskFanOut(ctx, core.FanOutDispatchTaskCommand{
		RunID:   cmd.RunID,
		TaskID:  cmd.TaskID,
		To:      cmd.To,
		Payload: cloneAnyMap(cmd.Payload),
	})
	if err != nil {
		return nil, err
	}
	return envelopes, nil
}

func (r *Runner) AckEnvelope(ctx context.Context, cmd api.AckEnvelopeCommand) error {
	return r.rt.AckEnvelope(ctx, core.AckEnvelopeCommand{EnvelopeID: cmd.EnvelopeID, HolderID: cmd.HolderID})
}

func (r *Runner) DeadLetter(ctx context.Context, cmd api.DeadLetterCommand) error {
	return r.rt.DeadLetter(ctx, core.DeadLetterCommand{EnvelopeID: cmd.EnvelopeID, Reason: cmd.Reason})
}

func (r *Runner) QueueEnvelope(ctx context.Context, env api.TaskEnvelope) error {
	return r.rt.QueueEnvelope(ctx, env)
}

func (r *Runner) LoadEnvelope(ctx context.Context, envelopeID string) (api.TaskEnvelope, error) {
	envelope, err := r.rt.LoadEnvelope(ctx, envelopeID)
	if err != nil {
		return api.TaskEnvelope{}, err
	}
	return envelope, nil
}

func (r *Runner) UpdateEnvelope(ctx context.Context, env api.TaskEnvelope) error {
	return r.rt.UpdateEnvelope(ctx, env)
}

func (r *Runner) ListEnvelopes(ctx context.Context, runID string) ([]api.TaskEnvelope, error) {
	envelopes, err := r.rt.ListEnvelopes(ctx, runID)
	if err != nil {
		return nil, err
	}
	return envelopes, nil
}
