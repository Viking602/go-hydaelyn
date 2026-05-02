package mailbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	commandbus "github.com/Viking602/go-hydaelyn/internal/command"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
	corestate "github.com/Viking602/go-hydaelyn/internal/core/state"
)

type TraceRecorder func(context.Context, ports.UnitOfWork, string, string, string, string) error

type DeadLetterHandlerOptions struct {
	Monitor     func() ports.TaskMonitor
	RecordTrace TraceRecorder
}

func RegisterDeadLetterHandler(bus *commandbus.Bus, options DeadLetterHandlerOptions) {
	commandbus.Register[DeadLetterCommand](bus, deadLetterHandler{options: options})
}

type deadLetterResult struct {
	Envelope       model.TaskEnvelope
	Task           model.Task
	Lease          model.TaskExecutionLease
	Decision       model.TaskMonitorDecision
	Reason         string
	Retry          bool
	TaskChanged    bool
	LeaseReleased  bool
	TaskTransition bool
}

type deadLetterHandler struct{ options DeadLetterHandlerOptions }

func (deadLetterHandler) Name() string { return DeadLetterCommand{}.CommandName() }

func (h deadLetterHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd DeadLetterCommand) (any, error) {
	env, err := uow.MailboxOutbox().LoadEnvelope(ctx, cmd.EnvelopeID)
	if err != nil {
		return nil, err
	}
	monitor, err := h.monitor()
	if err != nil {
		return nil, err
	}
	decision, err := monitor.DecideDeadLetter(ctx, env, cmd.Reason)
	if err != nil {
		return nil, err
	}
	if h.options.RecordTrace != nil {
		if err := h.options.RecordTrace(ctx, uow, env.RunID, env.TaskID, "task_monitor.dead_letter", "task_monitor"); err != nil {
			return nil, err
		}
	}
	if decision.Retry {
		return h.retry(ctx, uow, env, cmd.Reason, decision)
	}
	return h.dead(ctx, uow, env, cmd.Reason, decision)
}

func (h deadLetterHandler) monitor() (ports.TaskMonitor, error) {
	if h.options.Monitor == nil {
		return nil, fmt.Errorf("dead-letter handler missing task monitor: %w", model.ErrInvalidConfiguration)
	}
	monitor := h.options.Monitor()
	if monitor == nil {
		return nil, fmt.Errorf("dead-letter handler missing task monitor: %w", model.ErrInvalidConfiguration)
	}
	return monitor, nil
}

func (h deadLetterHandler) retry(ctx context.Context, uow ports.UnitOfWork, env model.TaskEnvelope, reason string, decision model.TaskMonitorDecision) (deadLetterResult, error) {
	env.Status = "pending"
	env.Attempts++
	backoff := env.RetryPolicy.Backoff
	if backoff <= 0 {
		backoff = time.Second
	}
	env.NextRetryAt = time.Now().UTC().Add(backoff)
	result := deadLetterResult{Envelope: env, Decision: decision, Reason: reason, Retry: true}
	task, err := uow.Tasks().LoadTask(ctx, env.RunID, env.TaskID)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return deadLetterResult{}, err
	}
	if err == nil && !corestate.IsTerminalTask(task.Status) {
		if lease, ok, err := uow.Leases().ActiveLeaseForTask(ctx, env.RunID, env.TaskID); err != nil {
			return deadLetterResult{}, err
		} else if ok && lease.Status == model.LeaseStatusActive {
			lease.Status = model.LeaseStatusReleased
			if err := uow.Leases().SaveLease(ctx, lease); err != nil {
				return deadLetterResult{}, err
			}
			if err := uow.Events().AppendEvent(ctx, model.Event{RunID: lease.RunID, TaskID: lease.TaskID, Type: model.EventTaskExecutionReleased, Payload: map[string]any{"leaseId": lease.ID}, RecordedAt: time.Now().UTC()}); err != nil {
				return deadLetterResult{}, err
			}
			result.Lease = lease
			result.LeaseReleased = true
		}
		if task.Status != model.TaskStatusDispatched {
			next, err := corestate.TransitionTask(task, model.TaskStatusDispatched, true)
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
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: env.RunID, TaskID: env.TaskID, Type: model.EventMailboxRetryScheduled, Payload: map[string]any{"envelopeId": env.ID, "reason": reason, "nextRetryAt": env.NextRetryAt}, RecordedAt: time.Now().UTC()}); err != nil {
		return deadLetterResult{}, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: env.RunID, TaskID: env.TaskID, Type: model.EventTaskMonitorDecision, Payload: map[string]any{"decision": decision.Decision, "reason": decision.Reason}, RecordedAt: time.Now().UTC()}); err != nil {
		return deadLetterResult{}, err
	}
	result.Envelope = env
	return result, nil
}

func (h deadLetterHandler) dead(ctx context.Context, uow ports.UnitOfWork, env model.TaskEnvelope, reason string, decision model.TaskMonitorDecision) (deadLetterResult, error) {
	env.Status = "dead"
	if err := uow.MailboxOutbox().UpdateEnvelope(ctx, env); err != nil {
		return deadLetterResult{}, err
	}
	task, err := uow.Tasks().LoadTask(ctx, env.RunID, env.TaskID)
	if err != nil {
		return deadLetterResult{}, err
	}
	result := deadLetterResult{Envelope: env, Task: task, Decision: decision, Reason: reason}
	if !corestate.IsTerminalTask(task.Status) {
		next, err := corestate.TransitionTask(task, model.TaskStatusBlocked, true)
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
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: env.RunID, TaskID: env.TaskID, Type: model.EventEnvelopeDeadLettered, Payload: map[string]any{"envelopeId": env.ID, "reason": reason}, RecordedAt: time.Now().UTC()}); err != nil {
		return deadLetterResult{}, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: env.RunID, TaskID: env.TaskID, Type: model.EventTaskMonitorDecision, Payload: map[string]any{"decision": decision.Decision, "reason": decision.Reason}, RecordedAt: time.Now().UTC()}); err != nil {
		return deadLetterResult{}, err
	}
	return result, nil
}
