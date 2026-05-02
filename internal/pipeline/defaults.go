package pipeline

import (
	"context"
	"time"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
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

func (defaultIntentAnalyzer) AnalyzeIntent(_ context.Context, run model.Run) (model.Intent, error) {
	return model.Intent{RunID: run.ID, Summary: run.Request}, nil
}

type defaultPlanner struct{}

func (defaultPlanner) CreatePlan(_ context.Context, intent model.Intent) (model.TodoPlan, error) {
	return model.TodoPlan{RunID: intent.RunID}, nil
}

type defaultPlanValidator struct{}

func (defaultPlanValidator) ValidatePlan(_ context.Context, _ model.TodoPlan) error {
	return nil
}

type defaultTaskRouter struct{}

func (defaultTaskRouter) RouteTasks(_ context.Context, plan model.TodoPlan) (model.RoutingPlan, error) {
	routing := model.RoutingPlan{RunID: plan.RunID, Routes: make([]model.TaskRoute, 0, len(plan.Tasks))}
	for _, task := range plan.Tasks {
		routing.Routes = append(routing.Routes, model.TaskRoute{
			TaskID:          task.ID,
			TargetAgentID:   task.OwnerAgentID,
			TargetComponent: task.OwnerComponent,
		})
	}
	return routing, nil
}

type defaultDispatcher struct{}

func (defaultDispatcher) Dispatch(_ context.Context, routing model.RoutingPlan) ([]model.TaskEnvelope, error) {
	envelopes := make([]model.TaskEnvelope, 0, len(routing.Routes))
	now := time.Now().UTC()
	for _, route := range routing.Routes {
		envelopes = append(envelopes, model.TaskEnvelope{
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

func (defaultTaskMonitor) Advance(context.Context, model.Run) error {
	return nil
}

func (defaultTaskMonitor) DecideDeadLetter(_ context.Context, env model.TaskEnvelope, reason string) (model.TaskMonitorDecision, error) {
	if env.RetryPolicy.MaxAttempts > 0 && env.Attempts < env.RetryPolicy.MaxAttempts {
		return model.TaskMonitorDecision{Decision: "retry", Reason: reason, Retry: true}, nil
	}
	return model.TaskMonitorDecision{Decision: "blocked", Reason: reason}, nil
}
