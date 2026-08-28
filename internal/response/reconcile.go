package response

import (
	"context"
	"time"

	"github.com/Viking602/venat/api"
	commandbus "github.com/Viking602/venat/internal/command"
	"github.com/Viking602/venat/internal/core/ports"
)

func RegisterReconcileHandler(bus *commandbus.Bus, options HandlerOptions) {
	commandbus.Register[ReconcilePublicationCommand](bus, reconcilePublicationHandler{options: options})
}

// ReconcilePublicationResult carries the message as it stands after the
// host's determination was applied.
type ReconcilePublicationResult struct {
	Message api.UserMessage
}

type reconcilePublicationHandler struct{ options HandlerOptions }

func (reconcilePublicationHandler) Name() string { return ReconcilePublicationCommand{}.CommandName() }

// Handle settles a message the runtime left in UserMessagePublishing. The
// runtime cannot resolve that state on its own — it does not know whether the
// gateway delivered the message before the process died — so the host reports
// the outcome and this handler applies it in one transaction.
func (h reconcilePublicationHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd ReconcilePublicationCommand) (any, error) {
	if cmd.RunID == "" || cmd.MessageID == "" {
		return nil, api.ErrInvalidCommand
	}
	message, err := uow.UserMessages().LoadMessage(ctx, cmd.RunID, cmd.MessageID)
	if err != nil {
		return nil, err
	}
	if h.options.Authorize != nil {
		if _, err := h.options.Authorize(ctx, uow, api.PolicyRequest{
			Operation: api.PolicyOperationResponseReconcile,
			RunID:     message.RunID,
			TaskID:    message.TaskID,
			Message:   &message,
		}); err != nil {
			return nil, err
		}
	}
	if message.Status != api.UserMessagePublishing {
		return settledReconciliation(message, cmd)
	}
	if cmd.Delivered {
		return h.markPublished(ctx, uow, message, cmd)
	}
	return h.returnToOutbox(ctx, uow, message, cmd)
}

// settledReconciliation makes a repeated reconciliation a no-op and a
// contradictory one an error: once a message is published the host cannot
// un-deliver it, and once it is back in the outbox it was never delivered.
func settledReconciliation(message api.UserMessage, cmd ReconcilePublicationCommand) (any, error) {
	switch {
	case message.Status == api.UserMessagePublished && cmd.Delivered:
		return ReconcilePublicationResult{Message: message}, nil
	case message.Status == api.UserMessageQueued && !cmd.Delivered:
		return ReconcilePublicationResult{Message: message}, nil
	case message.Status == api.UserMessagePublished || message.Status == api.UserMessageQueued:
		return nil, api.ErrIdempotencyConflict
	default:
		return nil, api.ErrInvalidCommand
	}
}

func (h reconcilePublicationHandler) markPublished(ctx context.Context, uow ports.UnitOfWork, message api.UserMessage, cmd ReconcilePublicationCommand) (any, error) {
	if h.options.RecordTrace != nil {
		if err := h.options.RecordTrace(ctx, uow, message.RunID, message.TaskID, "response.reconcile_publication", "response"); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()
	message.Status = api.UserMessagePublished
	message.PublishedAt = now
	message.UpdatedAt = now
	if err := uow.UserMessages().UpdateMessage(ctx, message); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{
		RunID:      message.RunID,
		TaskID:     message.TaskID,
		Type:       api.EventResponsePublished,
		Payload:    map[string]any{"messageId": message.ID, "message": UserMessagePayload(message), "reconciled": true, "reason": cmd.Reason},
		RecordedAt: now,
	}); err != nil {
		return nil, err
	}
	return ReconcilePublicationResult{Message: message}, nil
}

func (h reconcilePublicationHandler) returnToOutbox(ctx context.Context, uow ports.UnitOfWork, message api.UserMessage, cmd ReconcilePublicationCommand) (any, error) {
	now := time.Now().UTC()
	message.Status = api.UserMessageQueued
	message.UpdatedAt = now
	if err := uow.UserMessages().UpdateMessage(ctx, message); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{
		RunID:      message.RunID,
		TaskID:     message.TaskID,
		Type:       api.EventResponsePublishFailed,
		Payload:    map[string]any{"messageId": message.ID, "reason": cmd.Reason, "reconciled": true},
		RecordedAt: now,
	}); err != nil {
		return nil, err
	}
	return ReconcilePublicationResult{Message: message}, nil
}
