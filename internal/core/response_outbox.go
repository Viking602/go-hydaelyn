package core

import (
	"context"

	"github.com/Viking602/venat/internal/core/model"
)

// ResponseOutbox lists user messages for runID. Store errors are returned;
// an empty slice means the store confirmed there are no messages.
func (r *Runtime) ResponseOutbox(ctx context.Context, runID string) ([]model.UserMessage, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	messages, err := uow.UserMessages().ListMessages(ctx, runID)
	if err != nil {
		return nil, err
	}
	return messages, nil
}
