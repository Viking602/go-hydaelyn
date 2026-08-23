package core

import (
	"context"
	"errors"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/ports"
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
	if message.Status == api.UserMessagePublished {
		return nil
	}
	publishErr := r.currentOutputGateway().Publish(ctx, message)
	return r.publishResponseFinalize(ctx, cmd, message, publishErr)
}

func (r *Runtime) publishResponsePrepare(ctx context.Context, cmd PublishResponseCommand) (api.UserMessage, error) {
	uow, err := r.beginWriteUoW(ctx)
	if err != nil {
		return api.UserMessage{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = uow.Rollback(ctx)
		}
	}()
	message, err := uow.UserMessages().LoadMessage(ctx, cmd.RunID, cmd.MessageID)
	if err != nil {
		return api.UserMessage{}, err
	}
	if message.Status == api.UserMessagePublished {
		if err := uow.Commit(ctx); err != nil {
			return api.UserMessage{}, err
		}
		committed = true
		return message, nil
	}
	if message.Status != api.UserMessageQueued {
		return api.UserMessage{}, ErrInvalidCommand
	}
	decision, err := r.authorizeUoW(ctx, uow, api.PolicyRequest{
		Operation: api.PolicyOperationResponsePublish,
		RunID:     cmd.RunID,
		TaskID:    message.TaskID,
		Message:   &message,
	})
	if err != nil {
		if isCommitCommandError(err) {
			if commitErr := uow.Commit(ctx); commitErr != nil {
				return api.UserMessage{}, commitErr
			}
			committed = true
		}
		return api.UserMessage{}, err
	}
	message, err = r.enforceResponseUoW(ctx, uow, decision, message)
	if err != nil {
		if commitErr := uow.Commit(ctx); commitErr != nil {
			return api.UserMessage{}, commitErr
		}
		committed = true
		return api.UserMessage{}, err
	}
	if err := uow.UserMessages().UpdateMessage(ctx, message); err != nil {
		return api.UserMessage{}, err
	}
	if err := uow.Commit(ctx); err != nil {
		return api.UserMessage{}, err
	}
	committed = true
	return message, nil
}

func (r *Runtime) publishResponseFinalize(ctx context.Context, cmd PublishResponseCommand, message api.UserMessage, publishErr error) error {
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
		if appendErr := uow.Events().AppendEvent(ctx, api.Event{
			RunID:      cmd.RunID,
			TaskID:     message.TaskID,
			Type:       api.EventResponsePublishFailed,
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

func (r *Runtime) applyPublishedTransition(ctx context.Context, uow ports.UnitOfWork, cmd PublishResponseCommand, _ api.UserMessage) error {
	current, err := uow.UserMessages().LoadMessage(ctx, cmd.RunID, cmd.MessageID)
	if err != nil {
		return err
	}
	if current.Status == api.UserMessagePublished {
		return nil
	}
	if current.Status != api.UserMessageQueued {
		return ErrInvalidCommand
	}
	if err := r.recordEndedTraceUoW(ctx, uow, cmd.RunID, current.TaskID, "response.publish", "response"); err != nil {
		return err
	}
	now := time.Now().UTC()
	current.Status = api.UserMessagePublished
	current.PublishedAt = now
	current.UpdatedAt = now
	if err := uow.UserMessages().UpdateMessage(ctx, current); err != nil {
		return err
	}
	return uow.Events().AppendEvent(ctx, api.Event{
		RunID:      cmd.RunID,
		TaskID:     current.TaskID,
		Type:       api.EventResponsePublished,
		Payload:    map[string]any{"messageId": current.ID, "message": userMessagePayload(current)},
		RecordedAt: now,
	})
}
