package venat

import (
	"context"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core"
)

func (r *Runner) SubmitTypedReport(ctx context.Context, cmd api.SubmitTypedReportCommand) error {
	return r.rt.SubmitTypedReport(ctx, cmd)
}

func (r *Runner) SubmitUserInput(ctx context.Context, cmd api.SubmitUserInputCommand) error {
	return r.rt.SubmitUserInput(ctx, core.SubmitUserInputCommand(cmd))
}

func (r *Runner) SubmitResponseOutput(ctx context.Context, cmd api.SubmitResponseOutputCommand) error {
	return r.rt.SubmitResponseOutput(ctx, core.SubmitResponseOutputCommand{RunID: cmd.RunID, TaskID: cmd.TaskID, LeaseID: cmd.LeaseID, HolderType: api.HolderType(cmd.HolderType), HolderID: cmd.HolderID, TaskVersion: cmd.TaskVersion, Type: api.UserMessageType(cmd.Type), Title: cmd.Title, Payload: cmd.Payload, IdempotencyKey: cmd.IdempotencyKey})
}

func (r *Runner) PublishResponse(ctx context.Context, cmd api.PublishResponseCommand) error {
	return r.rt.PublishResponse(ctx, core.PublishResponseCommand(cmd))
}

// ReconcileResponsePublication settles a message left mid-publication by an
// interrupted publish. Such a message sits in api.UserMessagePublishing and
// the runtime will not republish it, because it cannot tell whether the
// output gateway already delivered it; the host reports that determination
// here. It returns the message as it stands afterwards.
//
// Call this only for crash residue. A publish claim carries no holder and no
// expiry, so the runtime cannot distinguish an abandoned claim from a live
// publisher's and will not judge staleness for you — reconciling a message
// whose publisher is still running races that publisher. See
// api.ReconcileResponsePublicationCommand for the full operational contract.
func (r *Runner) ReconcileResponsePublication(ctx context.Context, cmd api.ReconcileResponsePublicationCommand) (api.UserMessage, error) {
	return r.rt.ReconcileResponsePublication(ctx, core.ReconcileResponsePublicationCommand(cmd))
}

func (r *Runner) DrainResponseOutbox(ctx context.Context) (int, error) {
	published, err := r.rt.DrainResponseOutbox(ctx)
	return published, err
}

func (r *Runner) QueueMessage(ctx context.Context, message api.UserMessage) error {
	return r.rt.QueueMessage(ctx, message)
}

func (r *Runner) LoadMessage(ctx context.Context, runID, messageID string) (api.UserMessage, error) {
	message, err := r.rt.LoadMessage(ctx, runID, messageID)
	if err != nil {
		return api.UserMessage{}, err
	}
	return message, nil
}

func (r *Runner) UpdateMessage(ctx context.Context, message api.UserMessage) error {
	return r.rt.UpdateMessage(ctx, message)
}

func (r *Runner) ListMessages(ctx context.Context, runID string) ([]api.UserMessage, error) {
	messages, err := r.rt.ListMessages(ctx, runID)
	if err != nil {
		return nil, err
	}
	return append([]api.UserMessage(nil), messages...), nil
}

func (r *Runner) ListQueuedMessages(ctx context.Context) ([]api.UserMessage, error) {
	messages, err := r.rt.ListQueuedMessages(ctx)
	if err != nil {
		return nil, err
	}
	return append([]api.UserMessage(nil), messages...), nil
}
