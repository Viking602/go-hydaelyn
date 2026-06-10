package adapter

import (
	"context"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/internal/core"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

func ConfigToCore(config api.Config) core.Config {
	return core.Config{
		StoreProvider: StoreProviderToCore(config.StoreProvider),
		PolicyEngine:  PolicyEngineToCore(config.PolicyEngine),
		OutputGateway: OutputGatewayToCore(config.OutputGateway),
		Pipeline:      PipelineToCore(config.Pipeline),
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
