package core

import (
	"context"
	"time"

	commandbus "github.com/Viking602/go-hydaelyn/internal/core/command"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func registerExecutionUoWCommandHandlers(runtime *Runtime) {
	commandbus.Register[AcquireTaskExecutionCommand](runtime.commandBus, acquireTaskExecutionHandler{runtime: runtime})
	commandbus.Register[HeartbeatTaskExecutionCommand](runtime.commandBus, heartbeatTaskExecutionHandler{})
	commandbus.Register[ReleaseTaskExecutionCommand](runtime.commandBus, releaseTaskExecutionHandler{})
}

type acquireTaskExecutionHandler struct{ runtime *Runtime }

func (acquireTaskExecutionHandler) Name() string { return AcquireTaskExecutionCommand{}.CommandName() }

func (h acquireTaskExecutionHandler) Handle(ctx context.Context, uow ports.FullUnitOfWork, cmd AcquireTaskExecutionCommand) (any, error) {
	run, err := uow.Runs().LoadRun(ctx, cmd.RunID)
	if err != nil {
		return nil, err
	}
	if isTerminalRun(run.Status) {
		return nil, ErrTerminalState
	}
	task, err := uow.Tasks().LoadTask(ctx, cmd.RunID, cmd.TaskID)
	if err != nil {
		return nil, err
	}
	if isTerminalTask(task.Status) {
		return nil, ErrTerminalState
	}
	var env TaskEnvelope
	if cmd.EnvelopeID != "" {
		env, err = uow.MailboxOutbox().LoadEnvelope(ctx, cmd.EnvelopeID)
		if err != nil {
			return nil, err
		}
		if err := validateEnvelopeForAcquire(cmd, task, env); err != nil {
			return nil, err
		}
	} else if err := validateTaskHolder(task, cmd.HolderType, cmd.HolderID); err != nil {
		return nil, err
	}
	if lease, ok, err := uow.Leases().ActiveLeaseForTask(ctx, cmd.RunID, cmd.TaskID); err != nil {
		return nil, err
	} else if ok && lease.Status == LeaseStatusActive && lease.ExpiresAt.After(time.Now().UTC()) {
		return struct {
			Lease    TaskExecutionLease
			Acquired bool
		}{Lease: lease, Acquired: false}, nil
	}
	ttl := cmd.TTL
	if ttl <= 0 {
		ttl = time.Minute
	}
	now := time.Now().UTC()
	lease := TaskExecutionLease{
		ID:          h.runtime.newID("lease"),
		RunID:       cmd.RunID,
		TaskID:      cmd.TaskID,
		EnvelopeID:  cmd.EnvelopeID,
		HolderType:  cmd.HolderType,
		HolderID:    cmd.HolderID,
		TaskVersion: task.Version,
		AcquiredAt:  now,
		ExpiresAt:   now.Add(ttl),
		HeartbeatAt: now,
		Status:      LeaseStatusActive,
	}
	task, err = transitionTaskPure(task, TaskStatusRunning, false)
	if err != nil {
		return nil, err
	}
	task.Attempts++
	if err := uow.Tasks().SaveTask(ctx, task); err != nil {
		return nil, err
	}
	if cmd.EnvelopeID != "" {
		env.Status = "delivered"
		env.Attempts++
		env.DeliveredAt = now
		if err := uow.MailboxOutbox().UpdateEnvelope(ctx, env); err != nil {
			return nil, err
		}
	}
	if err := uow.Leases().SaveLease(ctx, lease); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: EventTaskExecutionAcquired, Payload: map[string]any{"leaseId": lease.ID, "envelopeId": cmd.EnvelopeID, "holderType": string(cmd.HolderType), "holderId": cmd.HolderID, "taskVersion": task.Version}, RecordedAt: now}); err != nil {
		return nil, err
	}
	return struct {
		Lease    TaskExecutionLease
		Acquired bool
	}{Lease: lease, Acquired: true}, nil
}

type heartbeatTaskExecutionHandler struct{}

func (heartbeatTaskExecutionHandler) Name() string {
	return HeartbeatTaskExecutionCommand{}.CommandName()
}

func (heartbeatTaskExecutionHandler) Handle(ctx context.Context, uow ports.FullUnitOfWork, cmd HeartbeatTaskExecutionCommand) (any, error) {
	lease, err := uow.Leases().LoadLease(ctx, cmd.LeaseID)
	if err != nil {
		return nil, err
	}
	if lease.Status != LeaseStatusActive {
		return nil, ErrLeaseNotActive
	}
	ttl := cmd.TTL
	if ttl <= 0 {
		ttl = time.Minute
	}
	now := time.Now().UTC()
	lease.HeartbeatAt = now
	lease.ExpiresAt = now.Add(ttl)
	if err := uow.Leases().SaveLease(ctx, lease); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: lease.RunID, TaskID: lease.TaskID, Type: EventTaskExecutionHeartbeat, Payload: map[string]any{"leaseId": lease.ID}, RecordedAt: now}); err != nil {
		return nil, err
	}
	return lease, nil
}

type releaseTaskExecutionHandler struct{}

func (releaseTaskExecutionHandler) Name() string { return ReleaseTaskExecutionCommand{}.CommandName() }

func (releaseTaskExecutionHandler) Handle(ctx context.Context, uow ports.FullUnitOfWork, cmd ReleaseTaskExecutionCommand) (any, error) {
	lease, err := uow.Leases().LoadLease(ctx, cmd.LeaseID)
	if err != nil {
		return nil, err
	}
	if cmd.HolderID != "" && lease.HolderID != cmd.HolderID {
		return nil, ErrLeaseHolderMismatch
	}
	lease.Status = LeaseStatusReleased
	if err := uow.Leases().SaveLease(ctx, lease); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: lease.RunID, TaskID: lease.TaskID, Type: EventTaskExecutionReleased, Payload: map[string]any{"leaseId": lease.ID}, RecordedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	return lease, nil
}

func validateEnvelopeForAcquire(cmd AcquireTaskExecutionCommand, task Task, env TaskEnvelope) error {
	if env.RunID != cmd.RunID || env.TaskID != cmd.TaskID {
		return ErrLeaseHolderMismatch
	}
	if env.TaskVersion != 0 && env.TaskVersion != task.Version {
		return ErrStaleTaskVersion
	}
	switch cmd.HolderType {
	case HolderAgent:
		if env.TargetAgentID != "" && env.TargetAgentID != cmd.HolderID {
			return ErrLeaseHolderMismatch
		}
	case HolderComponent:
		if env.TargetComponent != "" && env.TargetComponent != cmd.HolderID {
			return ErrLeaseHolderMismatch
		}
	}
	return nil
}
