package core

import "context"

func (r *Runtime) ResponseOutbox(runID string) []UserMessage {
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
