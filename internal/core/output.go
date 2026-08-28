package core

import (
	"context"
	"errors"

	"github.com/Viking602/venat/api"
)

type memoryOutputGateway struct{}

func (memoryOutputGateway) Publish(context.Context, api.UserMessage) error {
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

// DrainResponseOutbox publishes every queued user message and reports how
// many it published. The queued snapshot is taken outside a write
// transaction, so a message can be claimed by another publisher between the
// scan and the publish; such a message is skipped, not counted, and does not
// fail the drain.
func (r *Runtime) DrainResponseOutbox(ctx context.Context) (int, error) {
	messages, err := r.queuedResponseMessages(ctx)
	if err != nil {
		return 0, err
	}

	published := 0
	for _, message := range messages {
		if message.Status != api.UserMessageQueued {
			continue
		}
		didPublish, err := r.publishResponse(ctx, PublishResponseCommand{RunID: message.RunID, MessageID: message.ID})
		if didPublish {
			published++
		}
		if errors.Is(err, ErrResponsePublishInFlight) {
			continue
		}
		if err != nil {
			return published, err
		}
	}
	return published, nil
}

func (r *Runtime) queuedResponseMessages(ctx context.Context) ([]api.UserMessage, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = done() }()
	return uow.UserMessages().ListPendingFor(ctx, api.UserMessageSelector{
		Statuses: []string{string(api.UserMessageQueued)},
	})
}
