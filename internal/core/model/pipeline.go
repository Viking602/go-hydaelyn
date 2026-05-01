package model

// Intent describes the user's request as interpreted by the runtime pipeline.
type Intent struct {
	RunID   string         `json:"runId"`
	Summary string         `json:"summary,omitempty"`
	Fields  map[string]any `json:"fields,omitempty"`
}

type TodoPlan struct {
	RunID string `json:"runId"`
	Tasks []Task `json:"tasks,omitempty"`
}

type RoutingPlan struct {
	RunID  string      `json:"runId"`
	Routes []TaskRoute `json:"routes,omitempty"`
}

type TaskRoute struct {
	TaskID          string `json:"taskId"`
	TargetAgentID   string `json:"targetAgentId,omitempty"`
	TargetComponent string `json:"targetComponent,omitempty"`
}

type TaskMonitorDecision struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
	Retry    bool   `json:"retry,omitempty"`
}
