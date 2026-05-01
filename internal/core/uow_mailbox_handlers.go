package core

import (
	"context"
	"time"

	commandbus "github.com/Viking602/go-hydaelyn/internal/core/command"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func registerMailboxUoWCommandHandlers(runtime *Runtime) {
	commandbus.Register[AckEnvelopeCommand](runtime.commandBus, ackEnvelopeHandler{})
}

type ackEnvelopeHandler struct{}

func (ackEnvelopeHandler) Name() string { return AckEnvelopeCommand{}.CommandName() }

func (ackEnvelopeHandler) Handle(ctx context.Context, uow ports.FullUnitOfWork, cmd AckEnvelopeCommand) (any, error) {
	env, err := uow.MailboxOutbox().LoadEnvelope(ctx, cmd.EnvelopeID)
	if err != nil {
		return nil, err
	}
	if cmd.HolderID != "" && env.TargetAgentID != "" && env.TargetAgentID != cmd.HolderID {
		return nil, ErrLeaseHolderMismatch
	}
	env.Status = "acked"
	if err := uow.MailboxOutbox().UpdateEnvelope(ctx, env); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: env.RunID, TaskID: env.TaskID, Type: EventEnvelopeAcked, Payload: map[string]any{"envelopeId": cmd.EnvelopeID, "holderId": cmd.HolderID}, RecordedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	return env, nil
}
