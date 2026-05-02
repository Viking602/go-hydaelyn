package core

import (
	"context"

	blackboardsvc "github.com/Viking602/go-hydaelyn/internal/blackboard"
	commandbus "github.com/Viking602/go-hydaelyn/internal/core/command"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func registerBlackboardUoWCommandHandlers(runtime *Runtime) {
	commandbus.Register[WriteBlackboardItemCommand](runtime.commandBus, writeBlackboardItemHandler{runtime: runtime})
}

type writeBlackboardItemHandler struct{ runtime *Runtime }

func (writeBlackboardItemHandler) Name() string { return WriteBlackboardItemCommand{}.CommandName() }

func (h writeBlackboardItemHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd WriteBlackboardItemCommand) (any, error) {
	item := cmd.Item
	if _, err := h.runtime.authorizeUoW(ctx, uow, PolicyRequest{Operation: PolicyOperationBlackboardWrite, RunID: item.RunID, TaskID: item.TaskID, Actor: item.Source, Item: &item}); err != nil {
		return nil, err
	}
	return blackboardsvc.WriteItem(ctx, uow, h.runtime.newID, item)
}
