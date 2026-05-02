package execution

import (
	"context"

	commandbus "github.com/Viking602/go-hydaelyn/internal/command"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func RegisterHandlers(bus *commandbus.Bus, newID IDGenerator) {
	commandbus.Register[AcquireTaskExecutionCommand](bus, acquireTaskExecutionHandler{newID: newID})
	commandbus.Register[HeartbeatTaskExecutionCommand](bus, heartbeatTaskExecutionHandler{})
	commandbus.Register[ReleaseTaskExecutionCommand](bus, releaseTaskExecutionHandler{})
}

type acquireTaskExecutionHandler struct{ newID IDGenerator }

func (acquireTaskExecutionHandler) Name() string { return AcquireTaskExecutionCommand{}.CommandName() }

func (h acquireTaskExecutionHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd AcquireTaskExecutionCommand) (any, error) {
	result, err := Acquire(ctx, uow, h.newID, AcquireInput(cmd))
	if err != nil {
		return nil, err
	}
	return result, nil
}

type heartbeatTaskExecutionHandler struct{}

func (heartbeatTaskExecutionHandler) Name() string {
	return HeartbeatTaskExecutionCommand{}.CommandName()
}

func (heartbeatTaskExecutionHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd HeartbeatTaskExecutionCommand) (any, error) {
	return Heartbeat(ctx, uow, cmd.LeaseID, cmd.TTL)
}

type releaseTaskExecutionHandler struct{}

func (releaseTaskExecutionHandler) Name() string { return ReleaseTaskExecutionCommand{}.CommandName() }

func (releaseTaskExecutionHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd ReleaseTaskExecutionCommand) (any, error) {
	return Release(ctx, uow, cmd.LeaseID, cmd.HolderID)
}
