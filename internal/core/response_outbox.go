package core

import (
	"context"

	"github.com/Viking602/venat/api"
)

// ResponseOutbox lists user messages for runID. Store errors are returned;
// an empty slice means the store confirmed there are no messages.
func (r *Runtime) ResponseOutbox(ctx context.Context, runID string) (messages []api.UserMessage, err error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return nil, err
	}
	defer joinReadCleanup(&err, done)
	return uow.UserMessages().ListMessages(ctx, runID)
}
