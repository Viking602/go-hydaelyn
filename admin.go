package hydaelyn

import (
	"context"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/internal/core/adapter"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

func (r *Runner) RegisterAgent(profile api.AgentProfile) {
	r.rt.RegisterAgent(adapter.AgentProfileToModel(profile))
}

func (r *Runner) Agents() []api.AgentProfile {
	return adapter.AgentProfilesFromModel(r.rt.Agents())
}

func (r *Runner) RegisterTool(tool api.Tool) {
	r.rt.RegisterTool(adapter.ToolToModel(tool))
}

func (r *Runner) RegisterFlow(flow api.Flow) error {
	return adapter.ErrorToAPI(r.rt.RegisterFlow(adapter.FlowToModel(flow)))
}

func (r *Runner) SetMessagePolicy(policy api.MessagePolicyChecker) {
	if policy == nil {
		r.rt.SetMessagePolicy(nil)
		return
	}
	r.rt.SetMessagePolicy(func(message model.UserMessage) model.PolicyDecision {
		return adapter.PolicyDecisionToModel(policy(adapter.UserMessageFromModel(message)))
	})
}

func (r *Runner) SetPolicyEngine(policy api.PolicyEngine) {
	r.rt.SetPolicyEngine(adapter.PolicyEngineToCore(policy))
}

func (r *Runner) SetOutputGateway(gateway api.OutputGateway) {
	r.rt.SetOutputGateway(adapter.OutputGatewayToCore(gateway))
}

func (r *Runner) SetPipeline(components api.PipelineComponents) {
	r.rt.SetPipeline(adapter.PipelineToCore(components))
}

func (r *Runner) StoreProvider() api.StoreProvider {
	return adapter.StoreProviderFromCore(r.rt.StoreProvider())
}

func (r *Runner) StoreCapabilities(ctx context.Context) (api.StoreCapabilities, error) {
	capabilities, err := r.rt.StoreCapabilities(ctx)
	if err != nil {
		return api.StoreCapabilities{}, adapter.ErrorToAPI(err)
	}
	return api.StoreCapabilities{
		SupportsTransactions:        capabilities.SupportsTransactions,
		SupportsBlackboardSubscribe: capabilities.SupportsBlackboardSubscribe,
		SupportsListPending:         capabilities.SupportsListPending,
		SupportsConcurrentWriters:   capabilities.SupportsConcurrentWriters,
		SupportsDeadLetterRequeue:   capabilities.SupportsDeadLetterRequeue,
	}, nil
}

func (r *Runner) Close(ctx context.Context) error {
	return adapter.ErrorToAPI(r.rt.Close(ctx))
}

func (r *Runner) Begin(ctx context.Context) (api.UnitOfWork, error) {
	uow, err := r.rt.Begin(ctx)
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return adapter.UnitOfWorkFromCore(uow), nil
}

func (r *Runner) SaveRun(ctx context.Context, run api.Run) error {
	return adapter.ErrorToAPI(r.rt.SaveRun(ctx, adapter.RunToModel(run)))
}

func (r *Runner) LoadRun(ctx context.Context, runID string) (api.Run, error) {
	run, err := r.rt.LoadRun(ctx, runID)
	if err != nil {
		return api.Run{}, adapter.ErrorToAPI(err)
	}
	return adapter.RunFromModel(run), nil
}

func (r *Runner) AppendEvent(ctx context.Context, event api.Event) error {
	return adapter.ErrorToAPI(r.rt.AppendEvent(ctx, adapter.EventToModel(event)))
}

func (r *Runner) ListEvents(ctx context.Context, runID string) ([]api.Event, error) {
	events, err := r.rt.ListEvents(ctx, runID)
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return adapter.EventsFromModel(events), nil
}
