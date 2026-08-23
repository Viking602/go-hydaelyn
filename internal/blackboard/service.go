package blackboard

import (
	"context"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/ports"
)

type IDGenerator func(prefix string) string

func WriteItem(ctx context.Context, uow ports.UnitOfWork, newID IDGenerator, item api.BlackboardItem) (api.BlackboardItem, error) {
	if item.ID == "" {
		item.ID = newID("bb")
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	if err := uow.Blackboard().WriteItem(ctx, item); err != nil {
		return api.BlackboardItem{}, err
	}
	now := time.Now().UTC()
	if err := uow.Trace().SaveTraceSpan(ctx, api.TraceSpan{RunID: item.RunID, TaskID: item.TaskID, Name: "blackboard.write", Component: "blackboard", Status: api.TraceSpanEnded, StartedAt: now, EndedAt: now}); err != nil {
		return api.BlackboardItem{}, err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: item.RunID, TaskID: item.TaskID, Type: api.EventBlackboardItemWritten, Payload: map[string]any{"itemId": item.ID, "sourceType": string(item.Source.Type), "sourceId": item.Source.ID, "visibility": string(item.Visibility), "key": item.Key}, RecordedAt: now}); err != nil {
		return api.BlackboardItem{}, err
	}
	return item, nil
}
