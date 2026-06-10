package core

import (
	"context"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

func (r *Runtime) ResponseOutbox(ctx context.Context, runID string) []model.UserMessage {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return nil
	}
	defer done()
	messages, err := uow.UserMessages().ListMessages(ctx, runID)
	if err != nil {
		return nil
	}
	return messages
}
