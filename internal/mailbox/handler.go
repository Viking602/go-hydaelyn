package mailbox

import (
	"context"

	commandbus "github.com/Viking602/go-hydaelyn/internal/command"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func RegisterAckHandler(bus *commandbus.Bus) {
	commandbus.Register[AckEnvelopeCommand](bus, ackEnvelopeHandler{})
}

type ackEnvelopeHandler struct{}

func (ackEnvelopeHandler) Name() string { return AckEnvelopeCommand{}.CommandName() }

func (ackEnvelopeHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd AckEnvelopeCommand) (any, error) {
	return Ack(ctx, uow, cmd.EnvelopeID, cmd.HolderID)
}
