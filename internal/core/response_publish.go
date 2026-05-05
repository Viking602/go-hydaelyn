package core

import (
	"context"
	"errors"
	"time"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

// PublishResponse drives the multi-phase response publication flow:
//
//	Phase 1: load + validate + authorize, then commit so the runtime lock
//	         is released before the gateway call.
//	Phase 2: invoke the configured output gateway without holding any lock,
//	         so gateway implementations may re-enter the runtime.
//	Phase 3: persist the success/failure outcome in a fresh UoW.
func (r *Runtime) PublishResponse(ctx context.Context, cmd PublishResponseCommand) error {
	message, err := r.publishResponsePrepare(ctx, cmd)
	if err != nil {
		return err
	}
	if message.Status == model.UserMessagePublished {
		return nil
	}
	publishErr := r.currentOutputGateway().Publish(ctx, message)
	return r.publishResponseFinalize(ctx, cmd, message, publishErr)
}

func (r *Runtime) publishResponsePrepare(ctx context.Context, cmd PublishResponseCommand) (model.UserMessage, error) {
	uow, err := r.beginWriteUoW(ctx)
	if err != nil {
		return model.UserMessage{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = uow.Rollback(ctx)
		}
	}()
	message, err := uow.UserMessages().LoadMessage(ctx, cmd.RunID, cmd.MessageID)
	if err != nil {
		return model.UserMessage{}, err
	}
	if message.Status == model.UserMessagePublished {
		if err := uow.Commit(ctx); err != nil {
			return model.UserMessage{}, err
		}
		committed = true
		return message, nil
	}
	if message.Status != model.UserMessageQueued {
		return model.UserMessage{}, ErrInvalidCommand
	}
	if _, err := r.authorizeUoW(ctx, uow, model.PolicyRequest{
		Operation: model.PolicyOperationResponsePublish,
		RunID:     cmd.RunID,
		TaskID:    message.TaskID,
		Message:   &message,
	}); err != nil {
		if isCommitCommandError(err) {
			if commitErr := uow.Commit(ctx); commitErr != nil {
				return model.UserMessage{}, commitErr
			}
			committed = true
		}
		return model.UserMessage{}, err
	}
	if err := uow.Commit(ctx); err != nil {
		return model.UserMessage{}, err
	}
	committed = true
	return message, nil
}

func (r *Runtime) publishResponseFinalize(ctx context.Context, cmd PublishResponseCommand, message model.UserMessage, publishErr error) error {
	uow, err := r.beginWriteUoW(ctx)
	if err != nil {
		if publishErr != nil {
			return errors.Join(publishErr, err)
		}
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = uow.Rollback(ctx)
		}
	}()
	if publishErr != nil {
		if appendErr := uow.Events().AppendEvent(ctx, model.Event{
			RunID:      cmd.RunID,
			TaskID:     message.TaskID,
			Type:       model.EventResponsePublishFailed,
			Payload:    map[string]any{"messageId": message.ID, "reason": publishErr.Error()},
			RecordedAt: time.Now().UTC(),
		}); appendErr != nil {
			return errors.Join(publishErr, appendErr)
		}
		if commitErr := uow.Commit(ctx); commitErr != nil {
			return errors.Join(publishErr, commitErr)
		}
		committed = true
		return publishErr
	}
	if err := r.applyPublishedTransition(ctx, uow, cmd, message); err != nil {
		return err
	}
	if err := uow.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

func (r *Runtime) applyPublishedTransition(ctx context.Context, uow ports.UnitOfWork, cmd PublishResponseCommand, _ model.UserMessage) error {
	current, err := uow.UserMessages().LoadMessage(ctx, cmd.RunID, cmd.MessageID)
	if err != nil {
		return err
	}
	if current.Status == model.UserMessagePublished {
		return nil
	}
	if current.Status != model.UserMessageQueued {
		return ErrInvalidCommand
	}
	if err := r.recordEndedTraceUoW(ctx, uow, cmd.RunID, current.TaskID, "response.publish", "response"); err != nil {
		return err
	}
	now := time.Now().UTC()
	current.Status = model.UserMessagePublished
	current.PublishedAt = now
	current.UpdatedAt = now
	if err := uow.UserMessages().UpdateMessage(ctx, current); err != nil {
		return err
	}
	return uow.Events().AppendEvent(ctx, model.Event{
		RunID:      cmd.RunID,
		TaskID:     current.TaskID,
		Type:       model.EventResponsePublished,
		Payload:    map[string]any{"messageId": current.ID, "message": userMessagePayload(current)},
		RecordedAt: now,
	})
}
