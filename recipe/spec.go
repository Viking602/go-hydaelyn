package recipe

import (
	"context"

	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/planner"
	"github.com/Viking602/go-hydaelyn/team"
)

type Spec struct {
	Name               string                     `json:"name,omitempty" yaml:"name,omitempty"`
	Pattern            string                     `json:"pattern,omitempty" yaml:"pattern,omitempty"`
	Planner            string                     `json:"planner,omitempty" yaml:"planner,omitempty"`
	SupervisorProfile  string                     `json:"supervisorProfile,omitempty" yaml:"supervisor_profile,omitempty"`
	WorkerProfiles     []string                   `json:"workerProfiles,omitempty" yaml:"worker_profiles,omitempty"`
	Input              map[string]any             `json:"input,omitempty" yaml:"input,omitempty"`
	Metadata           map[string]string          `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	SuccessCriteria    []string                   `json:"successCriteria,omitempty" yaml:"success_criteria,omitempty"`
	VerificationPolicy planner.VerificationPolicy `json:"verificationPolicy,omitempty" yaml:"verification_policy,omitempty"`
	Tasks              []Task                     `json:"tasks,omitempty" yaml:"tasks,omitempty"`
	Flow               []Step                     `json:"flow,omitempty" yaml:"flow,omitempty"`
}

type Task struct {
	ID                   string                  `json:"id" yaml:"id"`
	Kind                 string                  `json:"kind,omitempty" yaml:"kind,omitempty"`
	Title                string                  `json:"title,omitempty" yaml:"title,omitempty"`
	Input                string                  `json:"input,omitempty" yaml:"input,omitempty"`
	RequiredRole         team.Role               `json:"requiredRole,omitempty" yaml:"required_role,omitempty"`
	RequiredCapabilities []string                `json:"requiredCapabilities,omitempty" yaml:"required_capabilities,omitempty"`
	Budget               team.Budget             `json:"budget,omitempty" yaml:"budget,omitempty"`
	AssigneeAgentID      string                  `json:"assigneeAgentId,omitempty" yaml:"assignee_agent_id,omitempty"`
	DependsOn            []string                `json:"dependsOn,omitempty" yaml:"depends_on,omitempty"`
	Reads                []string                `json:"reads,omitempty" yaml:"reads,omitempty"`
	Writes               []string                `json:"writes,omitempty" yaml:"writes,omitempty"`
	Publish              []team.OutputVisibility `json:"publish,omitempty" yaml:"publish,omitempty"`
	VerifyClaims         []string                `json:"verifyClaims,omitempty" yaml:"verify_claims,omitempty"`
	ExchangeSchema       string                  `json:"exchangeSchema,omitempty" yaml:"exchange_schema,omitempty"`
	FailurePolicy        team.FailurePolicy      `json:"failurePolicy,omitempty" yaml:"failure_policy,omitempty"`
}

type Step struct {
	Mode                 string                  `json:"mode,omitempty" yaml:"mode,omitempty"`
	Task                 *Task                   `json:"task,omitempty" yaml:"task,omitempty"`
	Steps                []Step                  `json:"steps,omitempty" yaml:"steps,omitempty"`
	ForEach              []string                `json:"forEach,omitempty" yaml:"for_each,omitempty"`
	Sequential           bool                    `json:"sequential,omitempty" yaml:"sequential,omitempty"`
	Template             *Task                   `json:"template,omitempty" yaml:"template,omitempty"`
	ID                   string                  `json:"id,omitempty" yaml:"id,omitempty"`
	Title                string                  `json:"title,omitempty" yaml:"title,omitempty"`
	Input                string                  `json:"input,omitempty" yaml:"input,omitempty"`
	Kind                 string                  `json:"kind,omitempty" yaml:"kind,omitempty"`
	Tool                 string                  `json:"tool,omitempty" yaml:"tool,omitempty"`
	RequiredRole         team.Role               `json:"requiredRole,omitempty" yaml:"required_role,omitempty"`
	RequiredCapabilities []string                `json:"requiredCapabilities,omitempty" yaml:"required_capabilities,omitempty"`
	AssigneeAgentID      string                  `json:"assigneeAgentId,omitempty" yaml:"assignee_agent_id,omitempty"`
	DependsOn            []string                `json:"dependsOn,omitempty" yaml:"depends_on,omitempty"`
	Reads                []string                `json:"reads,omitempty" yaml:"reads,omitempty"`
	Writes               []string                `json:"writes,omitempty" yaml:"writes,omitempty"`
	Publish              []team.OutputVisibility `json:"publish,omitempty" yaml:"publish,omitempty"`
	VerifyClaims         []string                `json:"verifyClaims,omitempty" yaml:"verify_claims,omitempty"`
	ExchangeSchema       string                  `json:"exchangeSchema,omitempty" yaml:"exchange_schema,omitempty"`
	FailurePolicy        team.FailurePolicy      `json:"failurePolicy,omitempty" yaml:"failure_policy,omitempty"`
}

type Compiled struct {
	Request host.StartTeamRequest `json:"request"`
	Plan    planner.Plan          `json:"plan"`
}

type StaticPlanner struct {
	PlanSpec planner.Plan
}

func (p StaticPlanner) Plan(_ context.Context, _ planner.PlanRequest) (planner.Plan, error) {
	return p.PlanSpec, nil
}

func (p StaticPlanner) Review(_ context.Context, _ planner.ReviewInput) (planner.ReviewDecision, error) {
	return planner.ReviewDecision{Action: planner.ReviewActionContinue}, nil
}

func (p StaticPlanner) Replan(_ context.Context, _ planner.ReplanInput) (planner.Plan, error) {
	return p.PlanSpec, nil
}
