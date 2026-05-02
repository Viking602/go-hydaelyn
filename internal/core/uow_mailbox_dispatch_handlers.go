package core

import (
	"context"
	"strings"

	commandbus "github.com/Viking602/go-hydaelyn/internal/core/command"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
	"github.com/Viking602/go-hydaelyn/internal/mailbox"
)

func registerMailboxDispatchUoWCommandHandlers(runtime *Runtime) {
	commandbus.Register[DispatchTaskCommand](runtime.commandBus, dispatchTaskHandler{runtime: runtime})
	commandbus.Register[FanOutDispatchTaskCommand](runtime.commandBus, fanOutDispatchTaskHandler{runtime: runtime})
}

type dispatchTaskHandler struct{ runtime *Runtime }

func (dispatchTaskHandler) Name() string { return DispatchTaskCommand{}.CommandName() }

func (h dispatchTaskHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd DispatchTaskCommand) (any, error) {
	_, task, err := mailbox.LoadDispatchTarget(ctx, uow, cmd.RunID, cmd.TaskID)
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
	return mailbox.Dispatch(ctx, uow, h.runtime.newID, mailbox.DispatchInput{
		RunID:           cmd.RunID,
		TaskID:          cmd.TaskID,
		TargetAgentID:   cmd.TargetAgentID,
		TargetComponent: cmd.TargetComponent,
		Payload:         cmd.Payload,
	})
}

type fanOutDispatchTaskHandler struct{ runtime *Runtime }

func (fanOutDispatchTaskHandler) Name() string { return FanOutDispatchTaskCommand{}.CommandName() }

func (h fanOutDispatchTaskHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd FanOutDispatchTaskCommand) (any, error) {
	_, task, err := mailbox.LoadDispatchTarget(ctx, uow, cmd.RunID, cmd.TaskID)
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
	return mailbox.FanOut(ctx, uow, h.runtime.newID, cmd.RunID, cmd.TaskID, recipients, cmd.Payload)
}
