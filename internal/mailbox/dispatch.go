package mailbox

import (
	"context"
	"slices"
	"time"

	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
	corestate "github.com/Viking602/venat/internal/core/state"
	"github.com/Viking602/venat/internal/eventpayload"
)

type IDGenerator func(prefix string) string

type DispatchInput struct {
	RunID           string
	TaskID          string
	TargetAgentID   string
	TargetComponent string
	Payload         map[string]any
}

// LoadDispatchTarget loads run and task for dispatch, checking terminal state.
func LoadDispatchTarget(ctx context.Context, uow ports.UnitOfWork, runID, taskID string) (model.Run, model.Task, error) {
	run, err := uow.Runs().LoadRun(ctx, runID)
	if err != nil {
		return model.Run{}, model.Task{}, err
	}
	if corestate.IsTerminalRun(run.Status) {
		return model.Run{}, model.Task{}, model.ErrTerminalState
	}
	task, err := uow.Tasks().LoadTask(ctx, runID, taskID)
	if err != nil {
		return model.Run{}, model.Task{}, err
	}
	if corestate.IsTerminalTask(task.Status) {
		return model.Run{}, model.Task{}, model.ErrTerminalState
	}
	return run, task, nil
}

// EnsureDependenciesReady checks all DependsOn tasks are completed.
func EnsureDependenciesReady(ctx context.Context, uow ports.UnitOfWork, task model.Task) error {
	if len(task.DependsOn) == 0 {
		return nil
	}
	tasks, err := uow.Tasks().ListTasks(ctx, task.RunID)
	if err != nil {
		return err
	}
	byID := make(map[string]model.Task, len(tasks))
	for _, item := range tasks {
		byID[item.ID] = item
	}
	ready, fatal := corestate.DependencyGate(task, byID)
	if fatal {
		return model.ErrDependencyFailed
	}
	if !ready {
		return model.ErrDependencyUnmet
	}
	return nil
}

func Dispatch(ctx context.Context, uow ports.UnitOfWork, newID IDGenerator, input DispatchInput) (model.TaskEnvelope, error) {
	run, task, err := LoadDispatchTarget(ctx, uow, input.RunID, input.TaskID)
	if err != nil {
		return model.TaskEnvelope{}, err
	}
	if err := EnsureDependenciesReady(ctx, uow, task); err != nil {
		return model.TaskEnvelope{}, err
	}
	next, err := corestate.TransitionTask(task, model.TaskStatusDispatched, false)
	if err != nil {
		return model.TaskEnvelope{}, err
	}
	if err := uow.Tasks().SaveTask(ctx, next); err != nil {
		return model.TaskEnvelope{}, err
	}
	now := time.Now().UTC()
	env := model.TaskEnvelope{ID: newID("env"), RunID: run.ID, TaskID: task.ID, TargetAgentID: input.TargetAgentID, TargetComponent: input.TargetComponent, Payload: eventpayload.CloneAnyMap(input.Payload), Status: "pending", TaskVersion: next.Version, ReadSelectors: slices.Clone(next.ReadSelectors), WriteTargets: slices.Clone(next.WriteTargets), RetryPolicy: next.RetryPolicy, CreatedAt: now}
	if err := uow.MailboxOutbox().QueueEnvelope(ctx, env); err != nil {
		return model.TaskEnvelope{}, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: env.RunID, TaskID: env.TaskID, Type: model.EventTaskDispatched, Payload: map[string]any{"envelope": eventpayload.Envelope(env)}, RecordedAt: now}); err != nil {
		return model.TaskEnvelope{}, err
	}
	return env, nil
}

func FanOut(ctx context.Context, uow ports.UnitOfWork, newID IDGenerator, runID, taskID string, recipients []string, payload map[string]any) ([]model.TaskEnvelope, error) {
	run, task, err := LoadDispatchTarget(ctx, uow, runID, taskID)
	if err != nil {
		return nil, err
	}
	if err := EnsureDependenciesReady(ctx, uow, task); err != nil {
		return nil, err
	}
	next, err := corestate.TransitionTask(task, model.TaskStatusDispatched, false)
	if err != nil {
		return nil, err
	}
	if err := uow.Tasks().SaveTask(ctx, next); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := make([]model.TaskEnvelope, 0, len(recipients))
	for _, agentID := range recipients {
		env := model.TaskEnvelope{ID: newID("env"), RunID: run.ID, TaskID: task.ID, TargetAgentID: agentID, Payload: eventpayload.CloneAnyMap(payload), Status: "pending", TaskVersion: next.Version, ReadSelectors: slices.Clone(next.ReadSelectors), WriteTargets: slices.Clone(next.WriteTargets), RetryPolicy: next.RetryPolicy, CreatedAt: now}
		if err := uow.MailboxOutbox().QueueEnvelope(ctx, env); err != nil {
			return nil, err
		}
		if err := uow.Events().AppendEvent(ctx, model.Event{RunID: env.RunID, TaskID: env.TaskID, Type: model.EventTaskDispatched, Payload: map[string]any{"envelope": eventpayload.Envelope(env)}, RecordedAt: now}); err != nil {
			return nil, err
		}
		out = append(out, env)
	}
	return out, nil
}

func Ack(ctx context.Context, uow ports.UnitOfWork, envelopeID, holderID string) (model.TaskEnvelope, error) {
	env, err := uow.MailboxOutbox().LoadEnvelope(ctx, envelopeID)
	if err != nil {
		return model.TaskEnvelope{}, err
	}
	if holderID != "" && env.TargetAgentID != "" && env.TargetAgentID != holderID {
		return model.TaskEnvelope{}, model.ErrLeaseHolderMismatch
	}
	env.Status = "acked"
	if err := uow.MailboxOutbox().UpdateEnvelope(ctx, env); err != nil {
		return model.TaskEnvelope{}, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: env.RunID, TaskID: env.TaskID, Type: model.EventEnvelopeAcked, Payload: map[string]any{"envelopeId": envelopeID, "holderId": holderID}, RecordedAt: time.Now().UTC()}); err != nil {
		return model.TaskEnvelope{}, err
	}
	return env, nil
}
