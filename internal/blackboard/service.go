package blackboard

import (
	"context"
	"time"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

type IDGenerator func(prefix string) string

func WriteItem(ctx context.Context, uow ports.UnitOfWork, newID IDGenerator, item model.BlackboardItem) (model.BlackboardItem, error) {
	if item.ID == "" {
		item.ID = newID("bb")
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	if err := uow.Blackboard().WriteItem(ctx, item); err != nil {
		return model.BlackboardItem{}, err
	}
	now := time.Now().UTC()
	if err := uow.Trace().SaveTraceSpan(ctx, model.TraceSpan{RunID: item.RunID, TaskID: item.TaskID, Name: "blackboard.write", Component: "blackboard", Status: model.TraceSpanEnded, StartedAt: now, EndedAt: now}); err != nil {
		return model.BlackboardItem{}, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: item.RunID, TaskID: item.TaskID, Type: model.EventBlackboardItemWritten, Payload: map[string]any{"itemId": item.ID, "sourceType": string(item.Source.Type), "sourceId": item.Source.ID, "visibility": string(item.Visibility), "key": item.Key}, RecordedAt: now}); err != nil {
		return model.BlackboardItem{}, err
	}
	return item, nil
}
