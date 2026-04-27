package orchestrator

import "context"

type memoryOutputGateway struct{}

func (memoryOutputGateway) Publish(context.Context, UserMessage) error {
	return nil
}

func (r *Runtime) DrainResponseOutbox(ctx context.Context) (int, error) {
	r.mu.Lock()
	ids := append([]string{}, r.messagesByRun[""]...)
	runIDs := make([]string, 0, len(r.messagesByRun))
	for runID := range r.messagesByRun {
		runIDs = append(runIDs, runID)
	}
	for _, runID := range runIDs {
		if runID == "" {
			continue
		}
		ids = append(ids, r.messagesByRun[runID]...)
	}
	r.mu.Unlock()

	published := 0
	for _, id := range ids {
		r.mu.Lock()
		message, ok := r.messages[id]
		r.mu.Unlock()
		if !ok || message.Status != UserMessageQueued {
			continue
		}
		if err := r.PublishResponse(ctx, PublishResponseCommand{RunID: message.RunID, MessageID: id}); err != nil {
			return published, err
		}
		published++
	}
	return published, nil
}
