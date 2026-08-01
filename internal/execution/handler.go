package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	commandbus "github.com/Viking602/venat/internal/command"
	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
	corestate "github.com/Viking602/venat/internal/core/state"
)

const (
	maxExecutionCheckpointBytes      = 8 << 20
	maxExecutionCheckpointTotalBytes = 512 << 20
	maxExecutionCheckpointCount      = 4096
)

func RegisterHandlers(bus *commandbus.Bus, newID IDGenerator) {
	commandbus.Register[AcquireTaskExecutionCommand](bus, acquireTaskExecutionHandler{newID: newID})
	commandbus.Register[HeartbeatTaskExecutionCommand](bus, heartbeatTaskExecutionHandler{})
	commandbus.Register[ReleaseTaskExecutionCommand](bus, releaseTaskExecutionHandler{})
	commandbus.Register[AppendTaskExecutionEventCommand](bus, appendTaskExecutionEventHandler{})
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
	return Heartbeat(ctx, uow, cmd.LeaseID, cmd.HolderID, cmd.TTL)
}

type releaseTaskExecutionHandler struct{}

func (releaseTaskExecutionHandler) Name() string { return ReleaseTaskExecutionCommand{}.CommandName() }

func (releaseTaskExecutionHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd ReleaseTaskExecutionCommand) (any, error) {
	return Release(ctx, uow, cmd.LeaseID, cmd.HolderID)
}

type appendTaskExecutionEventHandler struct{}

func (appendTaskExecutionEventHandler) Name() string {
	return AppendTaskExecutionEventCommand{}.CommandName()
}

func (appendTaskExecutionEventHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd AppendTaskExecutionEventCommand) (any, error) {
	if cmd.Event.RunID != cmd.RunID || cmd.Event.TaskID != cmd.TaskID {
		return nil, model.ErrInvalidCommand
	}
	if !isLeasedExecutionEvent(cmd.Event.Type) {
		return nil, model.ErrInvalidCommand
	}
	cmd.Event.Sequence = 0
	if err := validateExecutionEventSubmission(ctx, uow, cmd); err != nil {
		return nil, err
	}
	if cmd.Event.Type == model.EventExecutionCheckpointed {
		encoded, err := json.Marshal(cmd.Event)
		if err != nil {
			return nil, fmt.Errorf("%w: encode checkpoint: %v", model.ErrCheckpointLimitExceeded, err)
		}
		if len(encoded) > maxExecutionCheckpointBytes {
			return nil, fmt.Errorf("%w: checkpoint is %d bytes (maximum %d)", model.ErrCheckpointLimitExceeded, len(encoded), maxExecutionCheckpointBytes)
		}
		events, err := uow.Events().ListEvents(ctx, cmd.RunID)
		if err != nil {
			return nil, err
		}
		count, total := 0, len(encoded)
		for _, event := range events {
			if event.Type != model.EventExecutionCheckpointed || event.TaskID != cmd.TaskID {
				continue
			}
			count++
			previous, err := json.Marshal(event)
			if err != nil {
				return nil, fmt.Errorf("%w: encode stored checkpoint: %v", model.ErrCheckpointLimitExceeded, err)
			}
			total += len(previous)
		}
		if count >= maxExecutionCheckpointCount || total > maxExecutionCheckpointTotalBytes {
			return nil, fmt.Errorf("%w: task has %d checkpoints using %d bytes", model.ErrCheckpointLimitExceeded, count, total)
		}
	}
	if err := uow.Events().AppendEvent(ctx, cmd.Event); err != nil {
		return nil, err
	}
	return nil, nil
}

func isLeasedExecutionEvent(eventType model.EventType) bool {
	return eventType == model.EventType("StepCompleted") ||
		eventType == model.EventExecutionCheckpointed
}

func validateExecutionEventSubmission(ctx context.Context, uow ports.UnitOfWork, cmd AppendTaskExecutionEventCommand) error {
	_, _, _, err := ValidateSubmission(ctx, uow, cmd.RunID, cmd.TaskID, cmd.LeaseID, cmd.HolderType, cmd.HolderID, cmd.TaskVersion)
	if err == nil {
		return nil
	}
	if cmd.Event.Type != model.EventExecutionCheckpointed ||
		(!errors.Is(err, model.ErrLeaseNotActive) && !errors.Is(err, model.ErrStaleTaskVersion)) {
		return err
	}
	run, loadErr := uow.Runs().LoadRun(ctx, cmd.RunID)
	if loadErr != nil {
		return loadErr
	}
	if corestate.IsTerminalRun(run.Status) {
		return model.ErrTerminalState
	}
	task, loadErr := uow.Tasks().LoadTask(ctx, cmd.RunID, cmd.TaskID)
	if loadErr != nil {
		return loadErr
	}
	if task.Version != cmd.TaskVersion {
		return model.ErrStaleTaskVersion
	}
	switch task.Status {
	case model.TaskStatusPaused, model.TaskStatusWaitingUserInput, model.TaskStatusReconcileRequired:
	default:
		return err
	}
	lease, loadErr := uow.Leases().LoadLease(ctx, cmd.LeaseID)
	if loadErr != nil {
		return loadErr
	}
	if lease.Status != model.LeaseStatusReleased ||
		lease.RunID != cmd.RunID || lease.TaskID != cmd.TaskID ||
		lease.HolderType != cmd.HolderType || lease.HolderID != cmd.HolderID {
		return err
	}
	latest, found, loadErr := uow.Leases().ActiveLeaseForTask(ctx, cmd.RunID, cmd.TaskID)
	if loadErr != nil {
		return loadErr
	}
	if !found || latest.ID != lease.ID || latest.Version != lease.Version {
		return model.ErrLeaseNotActive
	}
	return nil
}
