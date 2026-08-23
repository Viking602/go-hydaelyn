package mailbox

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/Viking602/venat/api"
	commandbus "github.com/Viking602/venat/internal/command"
	"github.com/Viking602/venat/internal/core/ports"
	corestate "github.com/Viking602/venat/internal/core/state"
	"github.com/Viking602/venat/internal/eventpayload"
	"github.com/Viking602/venat/internal/execution"
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
	Envelope       api.TaskEnvelope
	Task           api.Task
	Lease          api.TaskExecutionLease
	Decision       api.TaskMonitorDecision
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
	if env.Status == "dead" {
		return deadLetterResult{Envelope: env, Reason: cmd.Reason}, nil
	}
	if env.Status != "delivered" {
		env.Attempts++
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
		return nil, fmt.Errorf("dead-letter handler missing task monitor: %w", api.ErrInvalidConfiguration)
	}
	monitor := h.options.Monitor()
	if monitor == nil {
		return nil, fmt.Errorf("dead-letter handler missing task monitor: %w", api.ErrInvalidConfiguration)
	}
	return monitor, nil
}

func (h deadLetterHandler) retry(ctx context.Context, uow ports.UnitOfWork, env api.TaskEnvelope, reason string, decision api.TaskMonitorDecision) (deadLetterResult, error) {
	env.Status = "pending"
	backoff := env.RetryPolicy.Backoff
	if backoff <= 0 {
		backoff = time.Second
	}
	env.NextRetryAt = time.Now().UTC().Add(backoff)
	result := deadLetterResult{Envelope: env, Decision: decision, Reason: reason, Retry: true}
	task, err := uow.Tasks().LoadTask(ctx, env.RunID, env.TaskID)
	if err != nil && !errors.Is(err, api.ErrNotFound) {
		return deadLetterResult{}, err
	}
	if err == nil && !corestate.IsTerminalTask(task.Status) {
		if lease, ok, err := uow.Leases().ActiveLeaseForTask(ctx, env.RunID, env.TaskID); err != nil {
			return deadLetterResult{}, err
		} else if ok && lease.Status == api.LeaseStatusActive {
			lease, err = execution.Release(ctx, uow, lease.ID, lease.HolderID)
			if err != nil {
				return deadLetterResult{}, err
			}
			result.Lease = lease
			result.LeaseReleased = true
		}
		if task.Status != api.TaskStatusDispatched {
			next, err := corestate.TransitionTask(task, api.TaskStatusDispatched, true)
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
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: env.RunID, TaskID: env.TaskID, Type: api.EventMailboxRetryScheduled, Payload: map[string]any{"envelopeId": env.ID, "reason": reason, "nextRetryAt": env.NextRetryAt, "envelope": eventpayload.Envelope(env), "task": eventpayload.Task(task)}, RecordedAt: time.Now().UTC()}); err != nil {
		return deadLetterResult{}, err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: env.RunID, TaskID: env.TaskID, Type: api.EventTaskMonitorDecision, Payload: map[string]any{"decision": decision.Decision, "reason": decision.Reason}, RecordedAt: time.Now().UTC()}); err != nil {
		return deadLetterResult{}, err
	}
	result.Envelope = env
	return result, nil
}

func (h deadLetterHandler) dead(ctx context.Context, uow ports.UnitOfWork, env api.TaskEnvelope, reason string, decision api.TaskMonitorDecision) (deadLetterResult, error) {
	original := env
	env.Status = "dead"
	if err := uow.MailboxOutbox().UpdateEnvelope(ctx, env); err != nil {
		return deadLetterResult{}, err
	}
	task, err := uow.Tasks().LoadTask(ctx, env.RunID, env.TaskID)
	if err != nil {
		return deadLetterResult{}, err
	}
	result := deadLetterResult{Envelope: env, Task: task, Decision: decision, Reason: reason}
	if lease, ok, err := uow.Leases().ActiveLeaseForTask(ctx, env.RunID, env.TaskID); err != nil {
		return deadLetterResult{}, err
	} else if ok && lease.Status == api.LeaseStatusActive {
		lease, err = execution.Release(ctx, uow, lease.ID, lease.HolderID)
		if err != nil {
			return deadLetterResult{}, err
		}
		result.Lease = lease
		result.LeaseReleased = true
	}
	if !corestate.IsTerminalTask(task.Status) {
		next, err := corestate.TransitionTask(task, api.TaskStatusBlocked, true)
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
	if err := uow.DeadLetters().AppendDeadLetter(ctx, api.DeadLetterEntry{
		ID:         "deadletter-" + original.ID,
		EnvelopeID: original.ID,
		RunID:      original.RunID,
		TaskID:     original.TaskID,
		Reason:     reason,
		Attempts:   original.Attempts,
		Envelope:   original,
		Payload:    maps.Clone(original.Payload),
		CreatedAt:  time.Now().UTC(),
	}); err != nil {
		return deadLetterResult{}, err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: env.RunID, TaskID: env.TaskID, Type: api.EventEnvelopeDeadLettered, Payload: map[string]any{"envelopeId": env.ID, "reason": reason, "envelope": eventpayload.Envelope(env), "task": eventpayload.Task(result.Task)}, RecordedAt: time.Now().UTC()}); err != nil {
		return deadLetterResult{}, err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: env.RunID, TaskID: env.TaskID, Type: api.EventTaskMonitorDecision, Payload: map[string]any{"decision": decision.Decision, "reason": decision.Reason}, RecordedAt: time.Now().UTC()}); err != nil {
		return deadLetterResult{}, err
	}
	return result, nil
}
