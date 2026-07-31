package trace

import (
	"context"

	commandbus "github.com/Viking602/venat/internal/command"
	"github.com/Viking602/venat/internal/core/ports"
)

func RegisterHandlers(bus *commandbus.Bus, newID IDGenerator) {
	commandbus.Register[StartTraceSpanCommand](bus, startTraceSpanHandler{newID: newID})
	commandbus.Register[EndTraceSpanCommand](bus, endTraceSpanHandler{})
}

type startTraceSpanHandler struct {
	newID IDGenerator
}

func (h startTraceSpanHandler) Name() string { return StartTraceSpanCommand{}.CommandName() }

func (h startTraceSpanHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd StartTraceSpanCommand) (any, error) {
	return StartSpan(ctx, uow, h.newID, StartInput(cmd))
}

type endTraceSpanHandler struct{}

func (endTraceSpanHandler) Name() string { return EndTraceSpanCommand{}.CommandName() }

func (endTraceSpanHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd EndTraceSpanCommand) (any, error) {
	return EndSpan(ctx, uow, cmd.SpanID, cmd.Error)
}
