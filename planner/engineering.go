package planner

import (
	"fmt"
	"strings"

	"github.com/Viking602/go-hydaelyn/team"
)

const (
	engineeringWorkflowTemplateName = "engineering-workflow"
	engineeringWorkflowVerifyMode   = "engineering_workflow"
	engineeringWorkflowPlanTaskID   = "workflow-plan"
	engineeringWorkflowSynthTaskID  = "workflow-synthesize"
)

type EngineeringWorkflow struct{}

type engineeringWorkItem struct {
	ID           string
	Title        string
	Input        string
	RequiredRole team.Role
}

func NewEngineeringWorkflow() EngineeringWorkflow {
	return EngineeringWorkflow{}
}

func (EngineeringWorkflow) Template(request team.StartRequest) (Template, error) {
	return BuildEngineeringWorkflowTemplate(request)
}

func BuildEngineeringWorkflowTemplate(request team.StartRequest) (Template, error) {
	goal := engineeringGoal(request.Input)
	if strings.TrimSpace(goal) == "" {
		return Template{}, fmt.Errorf("engineering workflow requires a goal")
	}
	items := engineeringWorkItems(request.Input)
	if len(items) == 0 {
		return Template{}, fmt.Errorf("engineering workflow requires at least one implementation task")
	}

	plannedTasks := make([]TaskSpec, 0, 2+(len(items)*3))
	plannedTasks = append(plannedTasks, engineeringPlanTask(goal, items))
	for _, item := range items {
		plannedTasks = append(plannedTasks, engineeringImplementTask(item))
	}
	for _, item := range items {
		plannedTasks = append(plannedTasks, engineeringReviewTask(item))
	}
	for _, item := range items {
		plannedTasks = append(plannedTasks, engineeringVerifyTask(item))
	}
	plannedTasks = append(plannedTasks, engineeringSynthesizeTask(goal, items))

	templateName := request.Pattern
	if strings.TrimSpace(templateName) == "" {
		templateName = engineeringWorkflowTemplateName
	}

	return Template{
		Name: templateName,
		Goal: goal,
		VerificationPolicy: VerificationPolicy{
			Required: true,
			Mode:     engineeringWorkflowVerifyMode,
		},
		Tasks: plannedTasks,
	}, nil
}

func engineeringPlanTask(goal string, items []engineeringWorkItem) TaskSpec {
	writes := make([]string, 0, len(items))
	for _, item := range items {
		writes = append(writes, engineeringPlanWriteKey(item.ID))
	}
	return TaskSpec{
		ID:               engineeringWorkflowPlanTaskID,
		Kind:             string(team.TaskKindResearch),
		Stage:            team.TaskStagePlan,
		Title:            "plan engineering workflow",
		Input:            fmt.Sprintf("Plan the engineering workflow for: %s", goal),
		RequiredRole:     team.RoleSupervisor,
		Writes:           writes,
		Publish:          []team.OutputVisibility{team.OutputVisibilityShared, team.OutputVisibilityBlackboard},
		Namespace:        "plan.workflow",
		FailurePolicy:    team.FailurePolicyFailFast,
		VerifierRequired: false,
	}
}

func engineeringImplementTask(item engineeringWorkItem) TaskSpec {
	return TaskSpec{
		ID:               item.ID,
		Kind:             string(team.TaskKindResearch),
		Stage:            team.TaskStageImplement,
		Title:            item.Title,
		Input:            item.Input,
		RequiredRole:     item.RequiredRole,
		DependsOn:        []string{engineeringWorkflowPlanTaskID},
		Reads:            []string{engineeringPlanWriteKey(item.ID)},
		Writes:           []string{engineeringImplementWriteKey(item.ID)},
		Publish:          []team.OutputVisibility{team.OutputVisibilityShared, team.OutputVisibilityBlackboard},
		Namespace:        engineeringImplementNamespace(item.ID),
		VerifierRequired: true,
		FailurePolicy:    team.FailurePolicyFailFast,
	}
}

func engineeringReviewTask(item engineeringWorkItem) TaskSpec {
	return TaskSpec{
		ID:               item.ID + "-review",
		Kind:             string(team.TaskKindResearch),
		Stage:            team.TaskStageReview,
		Title:            "review " + item.Title,
		Input:            "Review the implementation and flag gaps before verification.",
		RequiredRole:     item.RequiredRole,
		DependsOn:        []string{item.ID},
		Reads:            []string{engineeringImplementWriteKey(item.ID)},
		Writes:           []string{engineeringReviewWriteKey(item.ID)},
		Publish:          []team.OutputVisibility{team.OutputVisibilityShared, team.OutputVisibilityBlackboard},
		Namespace:        engineeringReviewNamespace(item.ID),
		VerifierRequired: true,
		FailurePolicy:    team.FailurePolicyFailFast,
	}
}

func engineeringVerifyTask(item engineeringWorkItem) TaskSpec {
	return TaskSpec{
		ID:               item.ID + "-verify",
		Kind:             string(team.TaskKindVerify),
		Stage:            team.TaskStageVerify,
		Title:            "verify " + item.Title,
		Input:            "Validate the reviewed implementation and publish a pass or fail decision.",
		RequiredRole:     team.RoleVerifier,
		DependsOn:        []string{item.ID + "-review"},
		Reads:            []string{engineeringReviewWriteKey(item.ID)},
		Writes:           []string{engineeringVerifyWriteKey(item.ID)},
		Publish:          []team.OutputVisibility{team.OutputVisibilityShared, team.OutputVisibilityBlackboard},
		Namespace:        engineeringVerifyNamespace(item.ID),
		VerifierRequired: true,
		FailurePolicy:    team.FailurePolicyFailFast,
	}
}

func engineeringSynthesizeTask(goal string, items []engineeringWorkItem) TaskSpec {
	dependsOn := make([]string, 0, len(items))
	reads := make([]string, 0, len(items))
	for _, item := range items {
		dependsOn = append(dependsOn, item.ID+"-verify")
		reads = append(reads, engineeringVerifyWriteKey(item.ID))
	}
	return TaskSpec{
		ID:               engineeringWorkflowSynthTaskID,
		Kind:             string(team.TaskKindSynthesize),
		Stage:            team.TaskStageSynthesize,
		Title:            "synthesize verified engineering output",
		Input:            fmt.Sprintf("Synthesize the final engineering deliverable for: %s", goal),
		RequiredRole:     team.RoleSupervisor,
		DependsOn:        dependsOn,
		Reads:            reads,
		Writes:           []string{"synthesize.final"},
		Publish:          []team.OutputVisibility{team.OutputVisibilityShared},
		Namespace:        "synthesize.final",
		VerifierRequired: true,
		FailurePolicy:    team.FailurePolicyFailFast,
	}
}

func engineeringGoal(input map[string]any) string {
	if input == nil {
		return ""
	}
	if goal, _ := input["query"].(string); strings.TrimSpace(goal) != "" {
		return goal
	}
	goal, _ := input["goal"].(string)
	return goal
}

func engineeringWorkItems(input map[string]any) []engineeringWorkItem {
	if input == nil {
		return nil
	}
	for _, key := range []string{"workItems", "branches", "tasks", "subqueries"} {
		if items := engineeringWorkItemsFromValue(input[key]); len(items) > 0 {
			return items
		}
	}
	if goal := engineeringGoal(input); strings.TrimSpace(goal) != "" {
		return []engineeringWorkItem{newEngineeringWorkItem(0, map[string]any{"title": goal, "input": goal})}
	}
	return nil
}

func engineeringWorkItemsFromValue(value any) []engineeringWorkItem {
	switch current := value.(type) {
	case []string:
		items := make([]engineeringWorkItem, 0, len(current))
		for idx, item := range current {
			items = append(items, newEngineeringWorkItem(idx, map[string]any{"title": item, "input": item}))
		}
		return items
	case []any:
		items := make([]engineeringWorkItem, 0, len(current))
		for idx, item := range current {
			switch cast := item.(type) {
			case string:
				items = append(items, newEngineeringWorkItem(idx, map[string]any{"title": cast, "input": cast}))
			case map[string]any:
				items = append(items, newEngineeringWorkItem(idx, cast))
			}
		}
		return items
	default:
		return nil
	}
}

func newEngineeringWorkItem(idx int, raw map[string]any) engineeringWorkItem {
	id, _ := raw["id"].(string)
	if strings.TrimSpace(id) == "" {
		id = fmt.Sprintf("implement-%d", idx+1)
	}
	title, _ := raw["title"].(string)
	input, _ := raw["input"].(string)
	if strings.TrimSpace(title) == "" {
		title = input
	}
	if strings.TrimSpace(input) == "" {
		input = title
	}
	requiredRole, _ := raw["requiredRole"].(team.Role)
	if requiredRole == "" {
		if text, ok := raw["requiredRole"].(string); ok {
			requiredRole = team.Role(text)
		}
	}
	if requiredRole == "" {
		requiredRole = team.RoleResearcher
	}
	return engineeringWorkItem{
		ID:           id,
		Title:        title,
		Input:        input,
		RequiredRole: requiredRole,
	}
}

func engineeringPlanWriteKey(taskID string) string {
	return "plan." + taskID
}

func engineeringImplementWriteKey(taskID string) string {
	return "implement." + taskID
}

func engineeringReviewWriteKey(taskID string) string {
	return "review." + taskID
}

func engineeringVerifyWriteKey(taskID string) string {
	return "verify." + taskID
}

func engineeringImplementNamespace(taskID string) string {
	return "impl." + taskID
}

func engineeringReviewNamespace(taskID string) string {
	return "review." + taskID
}

func engineeringVerifyNamespace(taskID string) string {
	return "verify." + taskID
}
