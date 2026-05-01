package core

import (
	"context"
	"time"

	commandbus "github.com/Viking602/go-hydaelyn/internal/core/command"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func registerBlackboardUoWCommandHandlers(runtime *Runtime) {
	commandbus.Register[WriteBlackboardItemCommand](runtime.commandBus, writeBlackboardItemHandler{runtime: runtime})
}

type writeBlackboardItemHandler struct{ runtime *Runtime }

func (writeBlackboardItemHandler) Name() string { return WriteBlackboardItemCommand{}.CommandName() }

func (h writeBlackboardItemHandler) Handle(ctx context.Context, uow ports.FullUnitOfWork, cmd WriteBlackboardItemCommand) (any, error) {
	item := cmd.Item
	if item.ID == "" {
		item.ID = h.runtime.newID("bb")
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	if _, err := h.runtime.authorizeUoW(ctx, uow, PolicyRequest{Operation: PolicyOperationBlackboardWrite, RunID: item.RunID, TaskID: item.TaskID, Actor: item.Source, Item: &item}); err != nil {
		return nil, err
	}
	if err := uow.Blackboard().WriteItem(ctx, item); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if err := uow.Trace().SaveTraceSpan(ctx, TraceSpan{RunID: item.RunID, TaskID: item.TaskID, Name: "blackboard.write", Component: "blackboard", Status: TraceSpanEnded, StartedAt: now, EndedAt: now}); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: item.RunID, TaskID: item.TaskID, Type: EventBlackboardItemWritten, Payload: map[string]any{"itemId": item.ID, "sourceType": string(item.Source.Type), "sourceId": item.Source.ID, "visibility": string(item.Visibility), "key": item.Key}, RecordedAt: now}); err != nil {
		return nil, err
	}
	return item, nil
}
