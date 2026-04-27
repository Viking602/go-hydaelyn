package orchestrator

import (
	"context"
	"time"
)

func defaultPipeline(config PipelineComponents) PipelineComponents {
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

func (defaultIntentAnalyzer) AnalyzeIntent(_ context.Context, run Run) (Intent, error) {
	return Intent{RunID: run.ID, Summary: run.Request}, nil
}

type defaultPlanner struct{}

func (defaultPlanner) CreatePlan(_ context.Context, intent Intent) (TodoPlan, error) {
	return TodoPlan{RunID: intent.RunID}, nil
}

type defaultPlanValidator struct{}

func (defaultPlanValidator) ValidatePlan(_ context.Context, _ TodoPlan) error {
	return nil
}

type defaultTaskRouter struct{}

func (defaultTaskRouter) RouteTasks(_ context.Context, plan TodoPlan) (RoutingPlan, error) {
	routing := RoutingPlan{RunID: plan.RunID, Routes: make([]TaskRoute, 0, len(plan.Tasks))}
	for _, task := range plan.Tasks {
		routing.Routes = append(routing.Routes, TaskRoute{
			TaskID:          task.ID,
			TargetAgentID:   task.OwnerAgentID,
			TargetComponent: task.OwnerComponent,
		})
	}
	return routing, nil
}

type defaultDispatcher struct{}

func (defaultDispatcher) Dispatch(_ context.Context, routing RoutingPlan) ([]TaskEnvelope, error) {
	envelopes := make([]TaskEnvelope, 0, len(routing.Routes))
	now := time.Now().UTC()
	for _, route := range routing.Routes {
		envelopes = append(envelopes, TaskEnvelope{
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

func (defaultTaskMonitor) Advance(context.Context, Run) error {
	return nil
}

func (defaultTaskMonitor) DecideDeadLetter(_ context.Context, env TaskEnvelope, reason string) (TaskMonitorDecision, error) {
	if env.RetryPolicy.MaxAttempts > 0 && env.Attempts < env.RetryPolicy.MaxAttempts {
		return TaskMonitorDecision{Decision: "retry", Reason: reason, Retry: true}, nil
	}
	return TaskMonitorDecision{Decision: "blocked", Reason: reason}, nil
}
