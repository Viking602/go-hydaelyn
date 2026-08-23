package ports

import "github.com/Viking602/venat/api"

type (
	IntentAnalyzer     = api.IntentAnalyzer
	Planner            = api.Planner
	PlanValidator      = api.PlanValidator
	TaskRouter         = api.TaskRouter
	Dispatcher         = api.Dispatcher
	TaskMonitor        = api.TaskMonitor
	PipelineComponents = api.PipelineComponents
)
