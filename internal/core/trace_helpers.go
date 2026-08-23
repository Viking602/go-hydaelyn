package core

import (
	"context"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/ports"
)

func (r *Runtime) recordEndedTraceUoW(ctx context.Context, uow ports.UnitOfWork, runID, taskID, name, component string) error {
	now := time.Now().UTC()
	return uow.Trace().SaveTraceSpan(ctx, api.TraceSpan{
		ID:        r.newID("span"),
		RunID:     runID,
		TaskID:    taskID,
		Name:      name,
		Component: component,
		Status:    api.TraceSpanEnded,
		StartedAt: now,
		EndedAt:   now,
	})
}
