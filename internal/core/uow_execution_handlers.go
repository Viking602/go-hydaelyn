package core

import (
	"context"

	commandbus "github.com/Viking602/go-hydaelyn/internal/core/command"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
	execution "github.com/Viking602/go-hydaelyn/internal/execution"
)

func registerExecutionUoWCommandHandlers(runtime *Runtime) {
	commandbus.Register[AcquireTaskExecutionCommand](runtime.commandBus, acquireTaskExecutionHandler{runtime: runtime})
	commandbus.Register[HeartbeatTaskExecutionCommand](runtime.commandBus, heartbeatTaskExecutionHandler{})
	commandbus.Register[ReleaseTaskExecutionCommand](runtime.commandBus, releaseTaskExecutionHandler{})
}

type acquireTaskExecutionHandler struct{ runtime *Runtime }

func (acquireTaskExecutionHandler) Name() string { return AcquireTaskExecutionCommand{}.CommandName() }

func (h acquireTaskExecutionHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd AcquireTaskExecutionCommand) (any, error) {
	result, err := execution.Acquire(ctx, uow, h.runtime.newID, execution.AcquireInput{
		RunID:      cmd.RunID,
		TaskID:     cmd.TaskID,
		EnvelopeID: cmd.EnvelopeID,
		HolderType: cmd.HolderType,
		HolderID:   cmd.HolderID,
		TTL:        cmd.TTL,
	})
	if err != nil {
		return nil, err
	}
	return struct {
		Lease    TaskExecutionLease
		Acquired bool
	}{Lease: result.Lease, Acquired: result.Acquired}, nil
}

type heartbeatTaskExecutionHandler struct{}

func (heartbeatTaskExecutionHandler) Name() string {
	return HeartbeatTaskExecutionCommand{}.CommandName()
}

func (heartbeatTaskExecutionHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd HeartbeatTaskExecutionCommand) (any, error) {
	return execution.Heartbeat(ctx, uow, cmd.LeaseID, cmd.TTL)
}

type releaseTaskExecutionHandler struct{}

func (releaseTaskExecutionHandler) Name() string { return ReleaseTaskExecutionCommand{}.CommandName() }

func (releaseTaskExecutionHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd ReleaseTaskExecutionCommand) (any, error) {
	return execution.Release(ctx, uow, cmd.LeaseID, cmd.HolderID)
}
