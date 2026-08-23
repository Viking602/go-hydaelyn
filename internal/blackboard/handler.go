package blackboard

import (
	"context"

	"github.com/Viking602/venat/api"
	commandbus "github.com/Viking602/venat/internal/command"
	"github.com/Viking602/venat/internal/core/ports"
)

type Authorizer func(context.Context, ports.UnitOfWork, api.PolicyRequest) (api.PolicyDecision, error)

type ObligationEnforcer func(context.Context, ports.UnitOfWork, api.PolicyDecision, api.BlackboardItem) (api.BlackboardItem, error)

type HandlerOptions struct {
	NewID              IDGenerator
	Authorize          Authorizer
	EnforceObligations ObligationEnforcer
}

func RegisterHandlers(bus *commandbus.Bus, options HandlerOptions) {
	commandbus.Register[WriteItemCommand](bus, writeItemHandler{options: options})
}

type writeItemHandler struct{ options HandlerOptions }

func (writeItemHandler) Name() string { return WriteItemCommand{}.CommandName() }

func (h writeItemHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd WriteItemCommand) (any, error) {
	item := cmd.Item
	if h.options.Authorize != nil {
		decision, err := h.options.Authorize(ctx, uow, api.PolicyRequest{Operation: api.PolicyOperationBlackboardWrite, RunID: item.RunID, TaskID: item.TaskID, Actor: item.Source, Item: &item})
		if err != nil {
			return nil, err
		}
		if h.options.EnforceObligations != nil {
			item, err = h.options.EnforceObligations(ctx, uow, decision, item)
			if err != nil {
				return nil, err
			}
		}
	}
	return WriteItem(ctx, uow, h.options.NewID, item)
}
