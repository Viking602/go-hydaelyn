package core

import (
	"context"

	commandbus "github.com/Viking602/go-hydaelyn/internal/core/command"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
	"github.com/Viking602/go-hydaelyn/internal/mailbox"
)

func registerMailboxUoWCommandHandlers(runtime *Runtime) {
	commandbus.Register[AckEnvelopeCommand](runtime.commandBus, ackEnvelopeHandler{})
}

type ackEnvelopeHandler struct{}

func (ackEnvelopeHandler) Name() string { return AckEnvelopeCommand{}.CommandName() }

func (ackEnvelopeHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd AckEnvelopeCommand) (any, error) {
	return mailbox.Ack(ctx, uow, cmd.EnvelopeID, cmd.HolderID)
}
