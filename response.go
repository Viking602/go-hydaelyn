package hydaelyn

import (
	"context"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/internal/core"
	"github.com/Viking602/go-hydaelyn/internal/core/adapter"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

func (r *Runner) SubmitTypedReport(ctx context.Context, cmd api.SubmitTypedReportCommand) error {
	return adapter.ErrorToAPI(r.rt.SubmitTypedReport(ctx, adapter.SubmitTypedReportCommandToCore(cmd)))
}

func (r *Runner) SubmitUserInput(ctx context.Context, cmd api.SubmitUserInputCommand) error {
	return adapter.ErrorToAPI(r.rt.SubmitUserInput(ctx, core.SubmitUserInputCommand{RunID: cmd.RunID, TaskID: cmd.TaskID, Input: cmd.Input}))
}

func (r *Runner) SubmitResponseOutput(ctx context.Context, cmd api.SubmitResponseOutputCommand) error {
	return adapter.ErrorToAPI(r.rt.SubmitResponseOutput(ctx, core.SubmitResponseOutputCommand{RunID: cmd.RunID, TaskID: cmd.TaskID, LeaseID: cmd.LeaseID, HolderType: model.HolderType(cmd.HolderType), HolderID: cmd.HolderID, TaskVersion: cmd.TaskVersion, Type: model.UserMessageType(cmd.Type), Title: cmd.Title, Payload: cmd.Payload, IdempotencyKey: cmd.IdempotencyKey}))
}

func (r *Runner) PublishResponse(ctx context.Context, cmd api.PublishResponseCommand) error {
	return adapter.ErrorToAPI(r.rt.PublishResponse(ctx, core.PublishResponseCommand{RunID: cmd.RunID, MessageID: cmd.MessageID}))
}

func (r *Runner) DrainResponseOutbox(ctx context.Context) (int, error) {
	published, err := r.rt.DrainResponseOutbox(ctx)
	return published, adapter.ErrorToAPI(err)
}

// Deprecated: use ResponseOutboxContext.
func (r *Runner) ResponseOutbox(runID string) []api.UserMessage {
	return r.ResponseOutboxContext(context.Background(), runID)
}

func (r *Runner) ResponseOutboxContext(ctx context.Context, runID string) []api.UserMessage {
	return adapter.UserMessagesFromModel(r.rt.ResponseOutbox(ctx, runID))
}

func (r *Runner) QueueMessage(ctx context.Context, message api.UserMessage) error {
	return adapter.ErrorToAPI(r.rt.QueueMessage(ctx, adapter.UserMessageToModel(message)))
}

func (r *Runner) LoadMessage(ctx context.Context, runID, messageID string) (api.UserMessage, error) {
	message, err := r.rt.LoadMessage(ctx, runID, messageID)
	if err != nil {
		return api.UserMessage{}, adapter.ErrorToAPI(err)
	}
	return adapter.UserMessageFromModel(message), nil
}

func (r *Runner) UpdateMessage(ctx context.Context, message api.UserMessage) error {
	return adapter.ErrorToAPI(r.rt.UpdateMessage(ctx, adapter.UserMessageToModel(message)))
}

func (r *Runner) ListMessages(ctx context.Context, runID string) ([]api.UserMessage, error) {
	messages, err := r.rt.ListMessages(ctx, runID)
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return adapter.UserMessagesFromModel(messages), nil
}

func (r *Runner) ListQueuedMessages(ctx context.Context) ([]api.UserMessage, error) {
	messages, err := r.rt.ListQueuedMessages(ctx)
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return adapter.UserMessagesFromModel(messages), nil
}
