package mailbox

import (
	"context"
	"slices"
	"time"

	"github.com/Viking602/venat/api"
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
func LoadDispatchTarget(ctx context.Context, uow ports.UnitOfWork, runID, taskID string) (api.Run, api.Task, error) {
	run, err := uow.Runs().LoadRun(ctx, runID)
	if err != nil {
		return api.Run{}, api.Task{}, err
	}
	if corestate.IsTerminalRun(run.Status) {
		return api.Run{}, api.Task{}, api.ErrTerminalState
	}
	task, err := uow.Tasks().LoadTask(ctx, runID, taskID)
	if err != nil {
		return api.Run{}, api.Task{}, err
	}
	if corestate.IsTerminalTask(task.Status) {
		return api.Run{}, api.Task{}, api.ErrTerminalState
	}
	return run, task, nil
}

// EnsureDependenciesReady checks all DependsOn tasks are completed.
func EnsureDependenciesReady(ctx context.Context, uow ports.UnitOfWork, task api.Task) error {
	if len(task.DependsOn) == 0 {
		return nil
	}
	tasks, err := uow.Tasks().ListTasks(ctx, task.RunID)
	if err != nil {
		return err
	}
	byID := make(map[string]api.Task, len(tasks))
	for _, item := range tasks {
		byID[item.ID] = item
	}
	ready, fatal := corestate.DependencyGate(task, byID)
	if fatal {
		return api.ErrDependencyFailed
	}
	if !ready {
		return api.ErrDependencyUnmet
	}
	return nil
}

func Dispatch(ctx context.Context, uow ports.UnitOfWork, newID IDGenerator, input DispatchInput) (api.TaskEnvelope, error) {
	run, task, err := LoadDispatchTarget(ctx, uow, input.RunID, input.TaskID)
	if err != nil {
		return api.TaskEnvelope{}, err
	}
	if err := EnsureDependenciesReady(ctx, uow, task); err != nil {
		return api.TaskEnvelope{}, err
	}
	next, err := corestate.TransitionTask(task, api.TaskStatusDispatched, false)
	if err != nil {
		return api.TaskEnvelope{}, err
	}
	if err := uow.Tasks().SaveTask(ctx, next); err != nil {
		return api.TaskEnvelope{}, err
	}
	now := time.Now().UTC()
	env := api.TaskEnvelope{ID: newID("env"), RunID: run.ID, TaskID: task.ID, TargetAgentID: input.TargetAgentID, TargetComponent: input.TargetComponent, Payload: eventpayload.CloneAnyMap(input.Payload), Status: "pending", TaskVersion: next.Version, ReadSelectors: slices.Clone(next.ReadSelectors), WriteTargets: slices.Clone(next.WriteTargets), RetryPolicy: next.RetryPolicy, CreatedAt: now}
	if err := uow.MailboxOutbox().QueueEnvelope(ctx, env); err != nil {
		return api.TaskEnvelope{}, err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: env.RunID, TaskID: env.TaskID, Type: api.EventTaskDispatched, Payload: map[string]any{"envelope": eventpayload.Envelope(env)}, RecordedAt: now}); err != nil {
		return api.TaskEnvelope{}, err
	}
	return env, nil
}

func FanOut(ctx context.Context, uow ports.UnitOfWork, newID IDGenerator, runID, taskID string, recipients []string, payload map[string]any) ([]api.TaskEnvelope, error) {
	run, task, err := LoadDispatchTarget(ctx, uow, runID, taskID)
	if err != nil {
		return nil, err
	}
	if err := EnsureDependenciesReady(ctx, uow, task); err != nil {
		return nil, err
	}
	next, err := corestate.TransitionTask(task, api.TaskStatusDispatched, false)
	if err != nil {
		return nil, err
	}
	if err := uow.Tasks().SaveTask(ctx, next); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := make([]api.TaskEnvelope, 0, len(recipients))
	for _, agentID := range recipients {
		env := api.TaskEnvelope{ID: newID("env"), RunID: run.ID, TaskID: task.ID, TargetAgentID: agentID, Payload: eventpayload.CloneAnyMap(payload), Status: "pending", TaskVersion: next.Version, ReadSelectors: slices.Clone(next.ReadSelectors), WriteTargets: slices.Clone(next.WriteTargets), RetryPolicy: next.RetryPolicy, CreatedAt: now}
		if err := uow.MailboxOutbox().QueueEnvelope(ctx, env); err != nil {
			return nil, err
		}
		if err := uow.Events().AppendEvent(ctx, api.Event{RunID: env.RunID, TaskID: env.TaskID, Type: api.EventTaskDispatched, Payload: map[string]any{"envelope": eventpayload.Envelope(env)}, RecordedAt: now}); err != nil {
			return nil, err
		}
		out = append(out, env)
	}
	return out, nil
}

func Ack(ctx context.Context, uow ports.UnitOfWork, envelopeID, holderID string) (api.TaskEnvelope, error) {
	env, err := uow.MailboxOutbox().LoadEnvelope(ctx, envelopeID)
	if err != nil {
		return api.TaskEnvelope{}, err
	}
	if holderID != "" && env.TargetAgentID != "" && env.TargetAgentID != holderID {
		return api.TaskEnvelope{}, api.ErrLeaseHolderMismatch
	}
	env.Status = "acked"
	if err := uow.MailboxOutbox().UpdateEnvelope(ctx, env); err != nil {
		return api.TaskEnvelope{}, err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: env.RunID, TaskID: env.TaskID, Type: api.EventEnvelopeAcked, Payload: map[string]any{"envelopeId": envelopeID, "holderId": holderID}, RecordedAt: time.Now().UTC()}); err != nil {
		return api.TaskEnvelope{}, err
	}
	return env, nil
}
