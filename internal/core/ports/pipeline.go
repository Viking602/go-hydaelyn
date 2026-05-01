package ports

import (
	"context"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

type IntentAnalyzer interface {
	AnalyzeIntent(context.Context, model.Run) (model.Intent, error)
}

type Planner interface {
	CreatePlan(context.Context, model.Intent) (model.TodoPlan, error)
}

type PlanValidator interface {
	ValidatePlan(context.Context, model.TodoPlan) error
}

type TaskRouter interface {
	RouteTasks(context.Context, model.TodoPlan) (model.RoutingPlan, error)
}

type Dispatcher interface {
	Dispatch(context.Context, model.RoutingPlan) ([]model.TaskEnvelope, error)
}

type TaskMonitor interface {
	Advance(context.Context, model.Run) error
	DecideDeadLetter(context.Context, model.TaskEnvelope, string) (model.TaskMonitorDecision, error)
}

type PipelineComponents struct {
	IntentAnalyzer IntentAnalyzer
	Planner        Planner
	Validator      PlanValidator
	Router         TaskRouter
	Dispatcher     Dispatcher
	TaskMonitor    TaskMonitor
}
