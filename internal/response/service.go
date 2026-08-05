package response

import (
	"context"
	"time"

	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
)

func CriticalContextItem(id, runID, taskID string, source model.SourceIdentity, key, payload string) model.BlackboardItem {
	if source.Type == "" {
		source = model.SourceIdentity{Type: model.SourceSystem, ID: "orchestrator"}
	}
	return model.BlackboardItem{ID: id, RunID: runID, TaskID: taskID, Type: model.BlackboardItemContext, Source: source, Visibility: model.BlackboardVisibilityAgentVisible, Key: key, Content: payload, Payload: payload, CreatedAt: time.Now().UTC()}
}

func AppendBlackboardWrittenEvent(ctx context.Context, uow ports.UnitOfWork, item model.BlackboardItem) error {
	return uow.Events().AppendEvent(ctx, model.Event{RunID: item.RunID, TaskID: item.TaskID, Type: model.EventBlackboardItemWritten, Payload: map[string]any{"itemId": item.ID, "sourceType": string(item.Source.Type), "sourceId": item.Source.ID, "visibility": string(item.Visibility), "key": item.Key}, RecordedAt: time.Now().UTC()})
}
