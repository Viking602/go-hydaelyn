package core

import (
	"context"

	"github.com/Viking602/venat/api"
	mailboxsvc "github.com/Viking602/venat/internal/mailbox"
)

func (r *Runtime) DispatchTask(ctx context.Context, cmd DispatchTaskCommand) (api.TaskEnvelope, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return api.TaskEnvelope{}, err
	}
	env, ok := result.(api.TaskEnvelope)
	if !ok {
		return api.TaskEnvelope{}, ErrInvalidCommand
	}
	return env, nil
}

// DispatchTaskFanOut resolves cmd.To against the registered agent profiles
// and writes one envelope per recipient. The task transitions to Dispatched
// once (the receivers compete for the lease via AcquireTaskExecution).
func (r *Runtime) DispatchTaskFanOut(ctx context.Context, cmd FanOutDispatchTaskCommand) ([]api.TaskEnvelope, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return nil, err
	}
	envs, ok := result.([]api.TaskEnvelope)
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
