package core

import (
	"context"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

func (r *Runtime) ResponseOutbox(runID string) []model.UserMessage {
	ctx := context.Background()
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
