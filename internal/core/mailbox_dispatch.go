package core

import (
	"context"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
	mailboxsvc "github.com/Viking602/go-hydaelyn/internal/mailbox"
)

func (r *Runtime) DispatchTask(ctx context.Context, cmd DispatchTaskCommand) (model.TaskEnvelope, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return model.TaskEnvelope{}, err
	}
	env, ok := result.(model.TaskEnvelope)
	if !ok {
		return model.TaskEnvelope{}, ErrInvalidCommand
	}
	return env, nil
}

// DispatchTaskFanOut resolves cmd.To against the registered agent profiles
// and writes one envelope per recipient. The task transitions to Dispatched
// once (the receivers compete for the lease via AcquireTaskExecution).
func (r *Runtime) DispatchTaskFanOut(ctx context.Context, cmd FanOutDispatchTaskCommand) ([]model.TaskEnvelope, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return nil, err
	}
	envs, ok := result.([]model.TaskEnvelope)
	if !ok {
		return nil, ErrInvalidCommand
	}
	return envs, nil
}

func registerMailboxDispatchUoWCommandHandlers(runtime *Runtime) {
	mailboxsvc.RegisterDispatchHandlers(runtime.commandBus, mailboxsvc.DispatchHandlerOptions{
		NewID:       runtime.newID,
		Agents:      runtime.Agents,
		Authorize:   runtime.authorizeUoW,
		RecordTrace: runtime.recordEndedTraceUoW,
	})
}
