package core

import (
	"context"
	"errors"
	"time"

	commandbus "github.com/Viking602/go-hydaelyn/internal/core/command"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func registerDeadLetterUoWCommandHandlers(runtime *Runtime) {
	commandbus.Register[DeadLetterCommand](runtime.commandBus, deadLetterHandler{runtime: runtime})
}

type deadLetterResult struct {
	Envelope       TaskEnvelope
	Task           Task
	Lease          TaskExecutionLease
	Decision       TaskMonitorDecision
	Reason         string
	Retry          bool
	TaskChanged    bool
	LeaseReleased  bool
	TaskTransition bool
}

type deadLetterHandler struct{ runtime *Runtime }

func (deadLetterHandler) Name() string { return DeadLetterCommand{}.CommandName() }

func (h deadLetterHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd DeadLetterCommand) (any, error) {
	env, err := uow.MailboxOutbox().LoadEnvelope(ctx, cmd.EnvelopeID)
	if err != nil {
		return nil, err
	}
	decision, err := h.runtime.currentTaskMonitor().DecideDeadLetter(ctx, env, cmd.Reason)
	if err != nil {
		return nil, err
	}
	if err := h.runtime.recordEndedTraceUoW(ctx, uow, env.RunID, env.TaskID, "task_monitor.dead_letter", "task_monitor"); err != nil {
		return nil, err
	}
	if decision.Retry {
		return h.retry(ctx, uow, env, cmd.Reason, decision)
	}
	return h.dead(ctx, uow, env, cmd.Reason, decision)
}

func (h deadLetterHandler) retry(ctx context.Context, uow ports.UnitOfWork, env TaskEnvelope, reason string, decision TaskMonitorDecision) (deadLetterResult, error) {
	env.Status = "pending"
	env.Attempts++
	backoff := env.RetryPolicy.Backoff
	if backoff <= 0 {
		backoff = time.Second
	}
	env.NextRetryAt = time.Now().UTC().Add(backoff)
	result := deadLetterResult{Envelope: env, Decision: decision, Reason: reason, Retry: true}
	task, err := uow.Tasks().LoadTask(ctx, env.RunID, env.TaskID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return deadLetterResult{}, err
	}
	if err == nil && !isTerminalTask(task.Status) {
		if lease, ok, err := uow.Leases().ActiveLeaseForTask(ctx, env.RunID, env.TaskID); err != nil {
			return deadLetterResult{}, err
		} else if ok && lease.Status == LeaseStatusActive {
			lease.Status = LeaseStatusReleased
			if err := uow.Leases().SaveLease(ctx, lease); err != nil {
				return deadLetterResult{}, err
			}
			if err := uow.Events().AppendEvent(ctx, Event{RunID: lease.RunID, TaskID: lease.TaskID, Type: EventTaskExecutionReleased, Payload: map[string]any{"leaseId": lease.ID}, RecordedAt: time.Now().UTC()}); err != nil {
				return deadLetterResult{}, err
			}
			result.Lease = lease
			result.LeaseReleased = true
		}
		if task.Status != TaskStatusDispatched {
			next, err := transitionTaskPure(task, TaskStatusDispatched, true)
			if err != nil {
				return deadLetterResult{}, err
			}
			task = next
			if err := uow.Tasks().SaveTask(ctx, task); err != nil {
				return deadLetterResult{}, err
			}
			result.TaskChanged = true
			result.TaskTransition = true
		}
		env.TaskVersion = task.Version
		env.DeliveredAt = time.Time{}
		result.Task = task
	}
	if err := uow.MailboxOutbox().UpdateEnvelope(ctx, env); err != nil {
		return deadLetterResult{}, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: env.RunID, TaskID: env.TaskID, Type: EventMailboxRetryScheduled, Payload: map[string]any{"envelopeId": env.ID, "reason": reason, "nextRetryAt": env.NextRetryAt}, RecordedAt: time.Now().UTC()}); err != nil {
		return deadLetterResult{}, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: env.RunID, TaskID: env.TaskID, Type: EventTaskMonitorDecision, Payload: map[string]any{"decision": decision.Decision, "reason": decision.Reason}, RecordedAt: time.Now().UTC()}); err != nil {
		return deadLetterResult{}, err
	}
	result.Envelope = env
	return result, nil
}

func (h deadLetterHandler) dead(ctx context.Context, uow ports.UnitOfWork, env TaskEnvelope, reason string, decision TaskMonitorDecision) (deadLetterResult, error) {
	env.Status = "dead"
	if err := uow.MailboxOutbox().UpdateEnvelope(ctx, env); err != nil {
		return deadLetterResult{}, err
	}
	task, err := uow.Tasks().LoadTask(ctx, env.RunID, env.TaskID)
	if err != nil {
		return deadLetterResult{}, err
	}
	result := deadLetterResult{Envelope: env, Task: task, Decision: decision, Reason: reason}
	if !isTerminalTask(task.Status) {
		next, err := transitionTaskPure(task, TaskStatusBlocked, true)
		if err != nil {
			return deadLetterResult{}, err
		}
		next.Error = reason
		if err := uow.Tasks().SaveTask(ctx, next); err != nil {
			return deadLetterResult{}, err
		}
		result.Task = next
		result.TaskChanged = true
		result.TaskTransition = true
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: env.RunID, TaskID: env.TaskID, Type: EventEnvelopeDeadLettered, Payload: map[string]any{"envelopeId": env.ID, "reason": reason}, RecordedAt: time.Now().UTC()}); err != nil {
		return deadLetterResult{}, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: env.RunID, TaskID: env.TaskID, Type: EventTaskMonitorDecision, Payload: map[string]any{"decision": decision.Decision, "reason": decision.Reason}, RecordedAt: time.Now().UTC()}); err != nil {
		return deadLetterResult{}, err
	}
	return result, nil
}
