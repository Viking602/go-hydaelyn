package planner

import (
	"context"

	"github.com/Viking602/go-hydaelyn/team"
)

type VerificationPolicy struct {
	Required bool   `json:"required,omitempty"`
	Mode     string `json:"mode,omitempty"`
}

type TaskSpec struct {
	ID                   string                  `json:"id"`
	Kind                 string                  `json:"kind,omitempty"`
	Stage                team.TaskStage          `json:"stage,omitempty"`
	Title                string                  `json:"title,omitempty"`
	Input                string                  `json:"input,omitempty"`
	RequiredRole         team.Role               `json:"requiredRole,omitempty"`
	RequiredCapabilities []string                `json:"requiredCapabilities,omitempty"`
	Budget               team.Budget             `json:"budget,omitempty"`
	AssigneeAgentID      string                  `json:"assigneeAgentId,omitempty"`
	DependsOn            []string                `json:"dependsOn,omitempty"`
	Reads                []string                `json:"reads,omitempty"`
	Writes               []string                `json:"writes,omitempty"`
	Publish              []team.OutputVisibility `json:"publish,omitempty"`
	VerifyClaims         []string                `json:"verifyClaims,omitempty"`
	ExchangeSchema       string                  `json:"exchangeSchema,omitempty"`
	Namespace            string                  `json:"namespace,omitempty"`
	VerifierRequired     bool                    `json:"verifierRequired,omitempty"`
	FailurePolicy        team.FailurePolicy      `json:"failurePolicy,omitempty"`
}

type Template struct {
	Name               string             `json:"name,omitempty"`
	Goal               string             `json:"goal,omitempty"`
	VerificationPolicy VerificationPolicy `json:"verificationPolicy,omitempty"`
	Tasks              []TaskSpec         `json:"tasks,omitempty"`
}

type Plan struct {
	Goal               string             `json:"goal,omitempty"`
	Tasks              []TaskSpec         `json:"tasks,omitempty"`
	SuccessCriteria    []string           `json:"successCriteria,omitempty"`
	VerificationPolicy VerificationPolicy `json:"verificationPolicy,omitempty"`
	Metadata           map[string]string  `json:"metadata,omitempty"`
}

type PlanRequest struct {
	TeamID            string            `json:"teamId,omitempty"`
	Pattern           string            `json:"pattern,omitempty"`
	Planner           string            `json:"planner,omitempty"`
	Goal              string            `json:"goal,omitempty"`
	Input             map[string]any    `json:"input,omitempty"`
	SupervisorProfile string            `json:"supervisorProfile,omitempty"`
	WorkerProfiles    []string          `json:"workerProfiles,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	Template          Template          `json:"template,omitempty"`
}

type ReviewAction string

const (
	ReviewActionContinue ReviewAction = "continue"
	ReviewActionComplete ReviewAction = "complete"
	ReviewActionReplan   ReviewAction = "replan"
	ReviewActionAbort    ReviewAction = "abort"
	ReviewActionAskHuman ReviewAction = "ask_human"
	ReviewActionEscalate ReviewAction = "escalate"
)

type ReviewInput struct {
	State team.RunState `json:"state"`
}

type ReviewDecision struct {
	Action ReviewAction `json:"action"`
	Reason string       `json:"reason,omitempty"`
}

type ReplanInput struct {
	State team.RunState `json:"state"`
}

type Planner interface {
	Plan(ctx context.Context, request PlanRequest) (Plan, error)
	Review(ctx context.Context, input ReviewInput) (ReviewDecision, error)
	Replan(ctx context.Context, input ReplanInput) (Plan, error)
}

type TemplateProvider interface {
	PlanTemplate(request team.StartRequest) (Template, error)
}
