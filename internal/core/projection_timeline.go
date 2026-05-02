package core

import (
	"context"

	proj "github.com/Viking602/go-hydaelyn/internal/projection"
)

func (r *Runtime) RunTimeline(ctx context.Context, runID string) ([]RunTimelineItem, error) {
	events, err := r.RunEvents(ctx, runID)
	if err != nil {
		return nil, err
	}
	return proj.Timeline(events), nil
}
