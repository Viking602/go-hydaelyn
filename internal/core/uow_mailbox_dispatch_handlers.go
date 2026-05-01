package core

import (
	"context"
	"slices"
	"strings"
	"time"

	commandbus "github.com/Viking602/go-hydaelyn/internal/core/command"
	"github.com/Viking602/go-hydaelyn/internal/core/mailbox"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func registerMailboxDispatchUoWCommandHandlers(runtime *Runtime) {
	commandbus.Register[DispatchTaskCommand](runtime.commandBus, dispatchTaskHandler{runtime: runtime})
	commandbus.Register[FanOutDispatchTaskCommand](runtime.commandBus, fanOutDispatchTaskHandler{runtime: runtime})
}

type dispatchTaskHandler struct{ runtime *Runtime }

func (dispatchTaskHandler) Name() string { return DispatchTaskCommand{}.CommandName() }

func (h dispatchTaskHandler) Handle(ctx context.Context, uow ports.FullUnitOfWork, cmd DispatchTaskCommand) (any, error) {
	run, task, err := mailbox.LoadDispatchTarget(ctx, uow, cmd.RunID, cmd.TaskID)
	if err != nil {
		return nil, err
	}
	if err := mailbox.EnsureDependenciesReady(ctx, uow, task); err != nil {
		return nil, err
	}
	if _, err := h.runtime.authorizeUoW(ctx, uow, PolicyRequest{Operation: PolicyOperationDispatch, RunID: cmd.RunID, TaskID: cmd.TaskID, Actor: SourceIdentity{Type: SourceComponent, ID: "dispatcher"}, Metadata: map[string]string{"targetAgentId": cmd.TargetAgentID, "targetComponent": cmd.TargetComponent}}); err != nil {
		return nil, err
	}
	if err := h.runtime.recordEndedTraceUoW(ctx, uow, cmd.RunID, cmd.TaskID, "mailbox.dispatch", "mailbox"); err != nil {
		return nil, err
	}
	next, err := transitionTaskPure(task, TaskStatusDispatched, false)
	if err != nil {
		return nil, err
	}
	if err := uow.Tasks().SaveTask(ctx, next); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	env := TaskEnvelope{ID: h.runtime.newID("env"), RunID: run.ID, TaskID: task.ID, TargetAgentID: cmd.TargetAgentID, TargetComponent: cmd.TargetComponent, Payload: cloneAnyMap(cmd.Payload), Status: "pending", TaskVersion: next.Version, ReadSelectors: slices.Clone(next.ReadSelectors), WriteTargets: slices.Clone(next.WriteTargets), RetryPolicy: next.RetryPolicy, CreatedAt: now}
	if err := uow.MailboxOutbox().QueueEnvelope(ctx, env); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: env.RunID, TaskID: env.TaskID, Type: EventTaskDispatched, Payload: map[string]any{"envelope": envPayload(env)}, RecordedAt: now}); err != nil {
		return nil, err
	}
	return env, nil
}

type fanOutDispatchTaskHandler struct{ runtime *Runtime }

func (fanOutDispatchTaskHandler) Name() string { return FanOutDispatchTaskCommand{}.CommandName() }

func (h fanOutDispatchTaskHandler) Handle(ctx context.Context, uow ports.FullUnitOfWork, cmd FanOutDispatchTaskCommand) (any, error) {
	run, task, err := mailbox.LoadDispatchTarget(ctx, uow, cmd.RunID, cmd.TaskID)
	if err != nil {
		return nil, err
	}
	if err := mailbox.EnsureDependenciesReady(ctx, uow, task); err != nil {
		return nil, err
	}
	recipients, err := mailbox.ResolveRecipients(h.runtime.Agents(), cmd.To)
	if err != nil {
		return nil, err
	}
	if _, err := h.runtime.authorizeUoW(ctx, uow, PolicyRequest{Operation: PolicyOperationDispatch, RunID: cmd.RunID, TaskID: cmd.TaskID, Actor: SourceIdentity{Type: SourceComponent, ID: "dispatcher"}, Metadata: map[string]string{"addressKind": string(cmd.To.Kind), "recipients": strings.Join(recipients, ",")}}); err != nil {
		return nil, err
	}
	if err := h.runtime.recordEndedTraceUoW(ctx, uow, cmd.RunID, cmd.TaskID, "mailbox.dispatch_fanout", "mailbox"); err != nil {
		return nil, err
	}
	next, err := transitionTaskPure(task, TaskStatusDispatched, false)
	if err != nil {
		return nil, err
	}
	if err := uow.Tasks().SaveTask(ctx, next); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := make([]TaskEnvelope, 0, len(recipients))
	for _, agentID := range recipients {
		env := TaskEnvelope{ID: h.runtime.newID("env"), RunID: run.ID, TaskID: task.ID, TargetAgentID: agentID, Payload: cloneAnyMap(cmd.Payload), Status: "pending", TaskVersion: next.Version, ReadSelectors: slices.Clone(next.ReadSelectors), WriteTargets: slices.Clone(next.WriteTargets), RetryPolicy: next.RetryPolicy, CreatedAt: now}
		if err := uow.MailboxOutbox().QueueEnvelope(ctx, env); err != nil {
			return nil, err
		}
		if err := uow.Events().AppendEvent(ctx, Event{RunID: env.RunID, TaskID: env.TaskID, Type: EventTaskDispatched, Payload: map[string]any{"envelope": envPayload(env)}, RecordedAt: now}); err != nil {
			return nil, err
		}
		out = append(out, env)
	}
	return out, nil
}
