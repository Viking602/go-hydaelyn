package blackboard

import (
	"context"

	commandbus "github.com/Viking602/go-hydaelyn/internal/command"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

type Authorizer func(context.Context, ports.UnitOfWork, model.PolicyRequest) (model.PolicyDecision, error)

type HandlerOptions struct {
	NewID     IDGenerator
	Authorize Authorizer
}

func RegisterHandlers(bus *commandbus.Bus, options HandlerOptions) {
	commandbus.Register[WriteItemCommand](bus, writeItemHandler{options: options})
}

type writeItemHandler struct{ options HandlerOptions }

func (writeItemHandler) Name() string { return WriteItemCommand{}.CommandName() }

func (h writeItemHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd WriteItemCommand) (any, error) {
	item := cmd.Item
	if h.options.Authorize != nil {
		if _, err := h.options.Authorize(ctx, uow, model.PolicyRequest{Operation: model.PolicyOperationBlackboardWrite, RunID: item.RunID, TaskID: item.TaskID, Actor: item.Source, Item: &item}); err != nil {
			return nil, err
		}
	}
	return WriteItem(ctx, uow, h.options.NewID, item)
}
