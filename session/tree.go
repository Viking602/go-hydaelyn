package session

import (
	"context"

	"github.com/Viking602/venat/message"
)

func (s *Session) ContextMessages(ctx context.Context, leafID string) ([]message.Message, error) {
	if leafID == "" {
		return nil, nil
	}
	entries, err := s.store.ScanBranch(ctx, leafID)
	if err != nil {
		return nil, err
	}
	out := make([]message.Message, 0, len(entries))
	for _, entry := range entries {
		if entry.StopReason == "error" || entry.StopReason == "aborted" {
			continue
		}
		out = append(out, entry.Message)
	}
	return out, nil
}
