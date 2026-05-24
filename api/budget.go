package api

import "time"

// TaskBudget is the per-Task budget consumed by agent.Engine and summed
// by multiagent.Scheduler for team-level observability.
//
// Distinct from GovernancePolicy.Budget (declared on api.AgentDefinition),
// which is the agent-definition-scoped run quota. TaskBudget is the
// inner-loop, single-Task accounting unit; the governance Budget is the
// outer envelope that the runner enforces across runs of one agent.
//
// Spec anchor: docs/product-spec/v0.8.0/01-public-api.md §Change 4 and
// ADR-017.
type TaskBudget struct {
	MaxTokens    int64         `json:"maxTokens,omitempty"`
	MaxWallClock time.Duration `json:"maxWallClock,omitempty"`
	MaxToolCalls int           `json:"maxToolCalls,omitempty"`
	MaxSteps     int           `json:"maxSteps,omitempty"`
}
