package response

import (
	"context"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/ports"
)

func CriticalContextItem(id, runID, taskID string, source api.SourceIdentity, key, payload string) api.BlackboardItem {
	if source.Type == "" {
		source = api.SourceIdentity{Type: api.SourceSystem, ID: "orchestrator"}
	}
	return api.BlackboardItem{ID: id, RunID: runID, TaskID: taskID, Type: api.BlackboardItemContext, Source: source, Visibility: api.BlackboardVisibilityAgentVisible, Key: key, Content: payload, Payload: payload, CreatedAt: time.Now().UTC()}
}

func AppendBlackboardWrittenEvent(ctx context.Context, uow ports.UnitOfWork, item api.BlackboardItem) error {
	return uow.Events().AppendEvent(ctx, api.Event{RunID: item.RunID, TaskID: item.TaskID, Type: api.EventBlackboardItemWritten, Payload: map[string]any{"itemId": item.ID, "sourceType": string(item.Source.Type), "sourceId": item.Source.ID, "visibility": string(item.Visibility), "key": item.Key}, RecordedAt: time.Now().UTC()})
}
