package mailbox

import (
	"context"
	"strings"

	commandbus "github.com/Viking602/venat/internal/command"
	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
)

type AgentProvider func() []model.AgentProfile

type Authorizer func(context.Context, ports.UnitOfWork, model.PolicyRequest) (model.PolicyDecision, error)

type DispatchHandlerOptions struct {
	NewID       IDGenerator
	Agents      AgentProvider
	Authorize   Authorizer
	RecordTrace TraceRecorder
}

func RegisterDispatchHandlers(bus *commandbus.Bus, options DispatchHandlerOptions) {
	commandbus.Register[DispatchTaskCommand](bus, dispatchTaskHandler{options: options})
	commandbus.Register[FanOutDispatchTaskCommand](bus, fanOutDispatchTaskHandler{options: options})
}

type dispatchTaskHandler struct{ options DispatchHandlerOptions }

func (dispatchTaskHandler) Name() string { return DispatchTaskCommand{}.CommandName() }

func (h dispatchTaskHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd DispatchTaskCommand) (any, error) {
	_, task, err := LoadDispatchTarget(ctx, uow, cmd.RunID, cmd.TaskID)
	if err != nil {
		return nil, err
	}
	if err := EnsureDependenciesReady(ctx, uow, task); err != nil {
		return nil, err
	}
	if h.options.Authorize != nil {
		if _, err := h.options.Authorize(ctx, uow, model.PolicyRequest{Operation: model.PolicyOperationDispatch, RunID: cmd.RunID, TaskID: cmd.TaskID, Actor: model.SourceIdentity{Type: model.SourceComponent, ID: "dispatcher"}, Metadata: map[string]string{"targetAgentId": cmd.TargetAgentID, "targetComponent": cmd.TargetComponent}}); err != nil {
			return nil, err
		}
	}
	if h.options.RecordTrace != nil {
		if err := h.options.RecordTrace(ctx, uow, cmd.RunID, cmd.TaskID, "mailbox.dispatch", "mailbox"); err != nil {
			return nil, err
		}
	}
	return Dispatch(ctx, uow, h.options.NewID, DispatchInput(cmd))
}

type fanOutDispatchTaskHandler struct{ options DispatchHandlerOptions }

func (fanOutDispatchTaskHandler) Name() string { return FanOutDispatchTaskCommand{}.CommandName() }

func (h fanOutDispatchTaskHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd FanOutDispatchTaskCommand) (any, error) {
	_, task, err := LoadDispatchTarget(ctx, uow, cmd.RunID, cmd.TaskID)
	if err != nil {
		return nil, err
	}
	if err := EnsureDependenciesReady(ctx, uow, task); err != nil {
		return nil, err
	}
	recipients, err := ResolveRecipients(h.options.Agents(), cmd.To)
	if err != nil {
		return nil, err
	}
	if h.options.Authorize != nil {
		if _, err := h.options.Authorize(ctx, uow, model.PolicyRequest{Operation: model.PolicyOperationDispatch, RunID: cmd.RunID, TaskID: cmd.TaskID, Actor: model.SourceIdentity{Type: model.SourceComponent, ID: "dispatcher"}, Metadata: map[string]string{"addressKind": string(cmd.To.Kind), "recipients": strings.Join(recipients, ",")}}); err != nil {
			return nil, err
		}
	}
	if h.options.RecordTrace != nil {
		if err := h.options.RecordTrace(ctx, uow, cmd.RunID, cmd.TaskID, "mailbox.dispatch_fanout", "mailbox"); err != nil {
			return nil, err
		}
	}
	return FanOut(ctx, uow, h.options.NewID, cmd.RunID, cmd.TaskID, recipients, cmd.Payload)
}
