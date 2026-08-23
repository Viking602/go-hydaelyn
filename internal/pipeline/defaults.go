package pipeline

import (
	"context"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/ports"
)

func Default(config ports.PipelineComponents) ports.PipelineComponents {
	if config.IntentAnalyzer == nil {
		config.IntentAnalyzer = defaultIntentAnalyzer{}
	}
	if config.Planner == nil {
		config.Planner = defaultPlanner{}
	}
	if config.Validator == nil {
		config.Validator = defaultPlanValidator{}
	}
	if config.Router == nil {
		config.Router = defaultTaskRouter{}
	}
	if config.Dispatcher == nil {
		config.Dispatcher = defaultDispatcher{}
	}
	if config.TaskMonitor == nil {
		config.TaskMonitor = defaultTaskMonitor{}
	}
	return config
}

type defaultIntentAnalyzer struct{}

func (defaultIntentAnalyzer) AnalyzeIntent(_ context.Context, run api.Run) (api.Intent, error) {
	return api.Intent{RunID: run.ID, Summary: run.Request}, nil
}

type defaultPlanner struct{}

func (defaultPlanner) CreatePlan(_ context.Context, intent api.Intent) (api.TodoPlan, error) {
	return api.TodoPlan{RunID: intent.RunID}, nil
}

type defaultPlanValidator struct{}

func (defaultPlanValidator) ValidatePlan(_ context.Context, _ api.TodoPlan) error {
	return nil
}

type defaultTaskRouter struct{}

func (defaultTaskRouter) RouteTasks(_ context.Context, plan api.TodoPlan) (api.RoutingPlan, error) {
	routing := api.RoutingPlan{RunID: plan.RunID, Routes: make([]api.TaskRoute, 0, len(plan.Tasks))}
	for _, task := range plan.Tasks {
		routing.Routes = append(routing.Routes, api.TaskRoute{
			TaskID:          task.ID,
			TargetAgentID:   task.OwnerAgentID,
			TargetComponent: task.OwnerComponent,
		})
	}
	return routing, nil
}

type defaultDispatcher struct{}

func (defaultDispatcher) Dispatch(_ context.Context, routing api.RoutingPlan) ([]api.TaskEnvelope, error) {
	envelopes := make([]api.TaskEnvelope, 0, len(routing.Routes))
	now := time.Now().UTC()
	for _, route := range routing.Routes {
		envelopes = append(envelopes, api.TaskEnvelope{
			RunID:           routing.RunID,
			TaskID:          route.TaskID,
			TargetAgentID:   route.TargetAgentID,
			TargetComponent: route.TargetComponent,
			Type:            "TaskEnvelope",
			Status:          "pending",
			CreatedAt:       now,
		})
	}
	return envelopes, nil
}

type defaultTaskMonitor struct{}

func (defaultTaskMonitor) Advance(context.Context, api.Run) error {
	return nil
}

func (defaultTaskMonitor) DecideDeadLetter(_ context.Context, env api.TaskEnvelope, reason string) (api.TaskMonitorDecision, error) {
	if env.RetryPolicy.MaxAttempts > 0 && env.Attempts < env.RetryPolicy.MaxAttempts {
		return api.TaskMonitorDecision{Decision: "retry", Reason: reason, Retry: true}, nil
	}
	return api.TaskMonitorDecision{Decision: "blocked", Reason: reason}, nil
}
