package core

import (
	"context"
	"fmt"
)

type memoryOutputGateway struct{}

func (memoryOutputGateway) Publish(context.Context, UserMessage) error {
	return nil
}

func (r *Runtime) currentOutputGateway() OutputGateway {
	r.configMu.RLock()
	defer r.configMu.RUnlock()
	if r.outputGateway == nil {
		return memoryOutputGateway{}
	}
	return r.outputGateway
}

func (r *Runtime) DrainResponseOutbox(ctx context.Context) (int, error) {
	messages, err := r.queuedResponseMessages(ctx)
	if err != nil {
		return 0, err
	}

	published := 0
	for _, message := range messages {
		if message.Status != UserMessageQueued {
			continue
		}
		if err := r.PublishResponse(ctx, PublishResponseCommand{RunID: message.RunID, MessageID: message.ID}); err != nil {
			return published, err
		}
		published++
	}
	return published, nil
}

func (r *Runtime) queuedResponseMessages(ctx context.Context) ([]UserMessage, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	scanner, ok := uow.UserMessages().(UserMessageOutboxScanner)
	if !ok {
		return nil, fmt.Errorf("user message store does not support queued outbox scanning: %w", ErrInvalidConfiguration)
	}
	return scanner.ListQueuedMessages(ctx)
}
