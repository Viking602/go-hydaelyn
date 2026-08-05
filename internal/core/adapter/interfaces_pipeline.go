package adapter

import (
	"context"
	"encoding/json"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core"
	"github.com/Viking602/venat/internal/core/model"
)

func ConfigToCore(config api.Config) core.Config {
	return core.Config{
		StoreProvider:  StoreProviderToCore(config.StoreProvider),
		PolicyEngine:   PolicyEngineToCore(config.PolicyEngine),
		PolicyEnforcer: PolicyObligationEnforcerToCore(config.PolicyEnforcer),
		OutputGateway:  OutputGatewayToCore(config.OutputGateway),
		Pipeline:       PipelineToCore(config.Pipeline),
	}
}

func PolicyEngineToCore(inner api.PolicyEngine) core.PolicyEngine {
	if inner == nil {
		return nil
	}
	return apiPolicyEngineAdapter{inner: inner}
}

type apiPolicyEngineAdapter struct{ inner api.PolicyEngine }

func (a apiPolicyEngineAdapter) Authorize(ctx context.Context, request model.PolicyRequest) (model.PolicyDecision, error) {
	decision, err := a.inner.Authorize(ctx, PolicyRequestFromModel(request))
	if err != nil {
		return model.PolicyDecision{}, ErrorToCore(err)
	}
	return PolicyDecisionToModel(decision), nil
}

func PolicyObligationEnforcerToCore(inner api.PolicyObligationEnforcer) core.PolicyObligationEnforcer {
	if inner == nil {
		return nil
	}
	return apiPolicyObligationEnforcerAdapter{inner: inner}
}

type apiPolicyObligationEnforcerAdapter struct {
	inner api.PolicyObligationEnforcer
}

func (a apiPolicyObligationEnforcerAdapter) EnforceBlackboardRead(
	ctx context.Context,
	decision model.PolicyDecision,
	selector model.BlackboardSelector,
	items []model.BlackboardItem,
) (model.BlackboardSelector, []model.BlackboardItem, error) {
	enforcedSelector, enforcedItems, err := a.inner.EnforceBlackboardRead(
		ctx,
		PolicyDecisionFromModel(decision),
		BlackboardSelectorFromModel(selector),
		BlackboardItemsFromModel(items),
	)
	if err != nil {
		return model.BlackboardSelector{}, nil, ErrorToCore(err)
	}
	return BlackboardSelectorToModel(enforcedSelector), BlackboardItemsToModel(enforcedItems), nil
}

func (a apiPolicyObligationEnforcerAdapter) EnforceBlackboardWrite(
	ctx context.Context,
	decision model.PolicyDecision,
	item model.BlackboardItem,
) (model.BlackboardItem, error) {
	enforced, err := a.inner.EnforceBlackboardWrite(ctx, PolicyDecisionFromModel(decision), BlackboardItemFromModel(item))
	if err != nil {
		return model.BlackboardItem{}, ErrorToCore(err)
	}
	return BlackboardItemToModel(enforced), nil
}

func (a apiPolicyObligationEnforcerAdapter) EnforceToolResult(
	ctx context.Context,
	decision model.PolicyDecision,
	result json.RawMessage,
) (json.RawMessage, error) {
	enforced, err := a.inner.EnforceToolResult(ctx, PolicyDecisionFromModel(decision), result)
	return enforced, ErrorToCore(err)
}

func (a apiPolicyObligationEnforcerAdapter) EnforceHandoff(
	ctx context.Context,
	decision model.PolicyDecision,
	handoff model.HandoffRequest,
) (model.HandoffRequest, error) {
	enforced, err := a.inner.EnforceHandoff(ctx, PolicyDecisionFromModel(decision), HandoffRequestFromModel(handoff))
	if err != nil {
		return model.HandoffRequest{}, ErrorToCore(err)
	}
	return HandoffRequestToModel(enforced), nil
}

func (a apiPolicyObligationEnforcerAdapter) EnforceResponse(
	ctx context.Context,
	decision model.PolicyDecision,
	message model.UserMessage,
) (model.UserMessage, error) {
	enforced, err := a.inner.EnforceResponse(ctx, PolicyDecisionFromModel(decision), UserMessageFromModel(message))
	if err != nil {
		return model.UserMessage{}, ErrorToCore(err)
	}
	return UserMessageToModel(enforced), nil
}

func (a apiPolicyObligationEnforcerAdapter) EnforceTrace(
	ctx context.Context,
	decision model.PolicyDecision,
	span model.TraceSpan,
) (model.TraceSpan, bool, error) {
	enforced, visible, err := a.inner.EnforceTrace(ctx, PolicyDecisionFromModel(decision), TraceSpanFromModel(span))
	if err != nil {
		return model.TraceSpan{}, false, ErrorToCore(err)
	}
	return TraceSpanToModel(enforced), visible, nil
}

func OutputGatewayToCore(inner api.OutputGateway) core.OutputGateway {
	if inner == nil {
		return nil
	}
	return apiOutputGatewayAdapter{inner: inner}
}

type apiOutputGatewayAdapter struct{ inner api.OutputGateway }

func (a apiOutputGatewayAdapter) Publish(ctx context.Context, message model.UserMessage) error {
	return ErrorToCore(a.inner.Publish(ctx, UserMessageFromModel(message)))
}

func PipelineToCore(components api.PipelineComponents) core.PipelineComponents {
	return core.PipelineComponents{
		IntentAnalyzer: intentAnalyzerToCore(components.IntentAnalyzer),
		Planner:        plannerToCore(components.Planner),
		Validator:      planValidatorToCore(components.Validator),
		Router:         taskRouterToCore(components.Router),
		Dispatcher:     dispatcherToCore(components.Dispatcher),
		TaskMonitor:    taskMonitorToCore(components.TaskMonitor),
	}
}

func intentAnalyzerToCore(inner api.IntentAnalyzer) core.IntentAnalyzer {
	if inner == nil {
		return nil
	}
	return apiIntentAnalyzerAdapter{inner: inner}
}

type apiIntentAnalyzerAdapter struct{ inner api.IntentAnalyzer }

func (a apiIntentAnalyzerAdapter) AnalyzeIntent(ctx context.Context, run model.Run) (model.Intent, error) {
	intent, err := a.inner.AnalyzeIntent(ctx, RunFromModel(run))
	if err != nil {
		return model.Intent{}, ErrorToCore(err)
	}
	return IntentToModel(intent), nil
}

func plannerToCore(inner api.Planner) core.Planner {
	if inner == nil {
		return nil
	}
	return apiPlannerAdapter{inner: inner}
}

type apiPlannerAdapter struct{ inner api.Planner }

func (a apiPlannerAdapter) CreatePlan(ctx context.Context, intent model.Intent) (model.TodoPlan, error) {
	plan, err := a.inner.CreatePlan(ctx, IntentFromModel(intent))
	if err != nil {
		return model.TodoPlan{}, ErrorToCore(err)
	}
	return TodoPlanToModel(plan), nil
}

func planValidatorToCore(inner api.PlanValidator) core.PlanValidator {
	if inner == nil {
		return nil
	}
	return apiPlanValidatorAdapter{inner: inner}
}

type apiPlanValidatorAdapter struct{ inner api.PlanValidator }

func (a apiPlanValidatorAdapter) ValidatePlan(ctx context.Context, plan model.TodoPlan) error {
	return ErrorToCore(a.inner.ValidatePlan(ctx, TodoPlanFromModel(plan)))
}

func taskRouterToCore(inner api.TaskRouter) core.TaskRouter {
	if inner == nil {
		return nil
	}
	return apiTaskRouterAdapter{inner: inner}
}

type apiTaskRouterAdapter struct{ inner api.TaskRouter }

func (a apiTaskRouterAdapter) RouteTasks(ctx context.Context, plan model.TodoPlan) (model.RoutingPlan, error) {
	routing, err := a.inner.RouteTasks(ctx, TodoPlanFromModel(plan))
	if err != nil {
		return model.RoutingPlan{}, ErrorToCore(err)
	}
	return RoutingPlanToModel(routing), nil
}

func dispatcherToCore(inner api.Dispatcher) core.Dispatcher {
	if inner == nil {
		return nil
	}
	return apiDispatcherAdapter{inner: inner}
}

type apiDispatcherAdapter struct{ inner api.Dispatcher }

func (a apiDispatcherAdapter) Dispatch(ctx context.Context, routing model.RoutingPlan) ([]model.TaskEnvelope, error) {
	envelopes, err := a.inner.Dispatch(ctx, RoutingPlanFromModel(routing))
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return TaskEnvelopesToModel(envelopes), nil
}

func taskMonitorToCore(inner api.TaskMonitor) core.TaskMonitor {
	if inner == nil {
		return nil
	}
	return apiTaskMonitorAdapter{inner: inner}
}

type apiTaskMonitorAdapter struct{ inner api.TaskMonitor }

func (a apiTaskMonitorAdapter) Advance(ctx context.Context, run model.Run) error {
	return ErrorToCore(a.inner.Advance(ctx, RunFromModel(run)))
}

func (a apiTaskMonitorAdapter) DecideDeadLetter(ctx context.Context, env model.TaskEnvelope, reason string) (model.TaskMonitorDecision, error) {
	decision, err := a.inner.DecideDeadLetter(ctx, TaskEnvelopeFromModel(env), reason)
	if err != nil {
		return model.TaskMonitorDecision{}, ErrorToCore(err)
	}
	return TaskMonitorDecisionToModel(decision), nil
}
