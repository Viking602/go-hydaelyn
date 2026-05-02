package core

import (
	"context"

	mailboxsvc "github.com/Viking602/go-hydaelyn/internal/mailbox"
)

func (r *Runtime) AckEnvelope(ctx context.Context, cmd AckEnvelopeCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runtime) DeadLetter(ctx context.Context, cmd DeadLetterCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}

func registerMailboxUoWCommandHandlers(runtime *Runtime) {
	mailboxsvc.RegisterAckHandler(runtime.commandBus)
}

func registerDeadLetterUoWCommandHandlers(runtime *Runtime) {
	mailboxsvc.RegisterDeadLetterHandler(runtime.commandBus, mailboxsvc.DeadLetterHandlerOptions{
		Monitor:     runtime.currentTaskMonitor,
		RecordTrace: runtime.recordEndedTraceUoW,
	})
}
