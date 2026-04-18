package planner

import (
	"slices"
	"testing"

	"github.com/Viking602/go-hydaelyn/team"
)

func TestEngineeringWorkflow_MapsPhasesToPlannerTasks(t *testing.T) {
	workflow := NewEngineeringWorkflow()
	template, err := workflow.Template(team.StartRequest{
		Pattern: "collab",
		Input: map[string]any{
			"query": "ship the engineering workflow",
			"workItems": []any{
				map[string]any{"id": "api", "title": "implement API", "input": "Build the API contract", "requiredRole": string(team.RoleResearcher)},
				map[string]any{"id": "ui", "title": "implement UI", "input": "Build the UI flow", "requiredRole": string(team.RoleResearcher)},
			},
		},
	})
	if err != nil {
		t.Fatalf("Template() error = %v", err)
	}
	if template.Name != "collab" {
		t.Fatalf("expected collaboration pattern template name, got %q", template.Name)
	}
	if template.Goal != "ship the engineering workflow" {
		t.Fatalf("expected goal to carry through, got %#v", template.Goal)
	}
	if !template.VerificationPolicy.Required || template.VerificationPolicy.Mode != engineeringWorkflowVerifyMode {
		t.Fatalf("expected engineering verification policy, got %#v", template.VerificationPolicy)
	}
	if len(template.Tasks) != 8 {
		t.Fatalf("expected 8 workflow tasks, got %#v", template.Tasks)
	}

	plan := taskByID(t, template.Tasks, engineeringWorkflowPlanTaskID)
	if plan.Stage != team.TaskStagePlan || plan.RequiredRole != team.RoleSupervisor {
		t.Fatalf("expected supervisor planning task, got %#v", plan)
	}
	if len(plan.Writes) != 2 || !slices.Equal(plan.Writes, []string{"plan.api", "plan.ui"}) {
		t.Fatalf("expected plan task to create implementation plan outputs, got %#v", plan.Writes)
	}

	implement := taskByID(t, template.Tasks, "api")
	if implement.Stage != team.TaskStageImplement || implement.Namespace != "impl.api" {
		t.Fatalf("expected implementation task metadata, got %#v", implement)
	}
	if !slices.Equal(implement.DependsOn, []string{engineeringWorkflowPlanTaskID}) || !slices.Equal(implement.Reads, []string{"plan.api"}) {
		t.Fatalf("expected implementation task to depend on and read plan output, got %#v", implement)
	}

	review := taskByID(t, template.Tasks, "api-review")
	if review.Stage != team.TaskStageReview || review.Namespace != "review.api" {
		t.Fatalf("expected review task metadata, got %#v", review)
	}
	if !slices.Equal(review.DependsOn, []string{"api"}) || !slices.Equal(review.Reads, []string{"implement.api"}) {
		t.Fatalf("expected review task to consume implementation output, got %#v", review)
	}

	verify := taskByID(t, template.Tasks, "api-verify")
	if verify.Stage != team.TaskStageVerify || verify.RequiredRole != team.RoleVerifier || verify.Namespace != "verify.api" {
		t.Fatalf("expected verifier task metadata, got %#v", verify)
	}
	if !slices.Equal(verify.DependsOn, []string{"api-review"}) || !slices.Equal(verify.Reads, []string{"review.api"}) {
		t.Fatalf("expected verify task to consume review output, got %#v", verify)
	}

	synthesize := taskByID(t, template.Tasks, engineeringWorkflowSynthTaskID)
	if synthesize.Stage != team.TaskStageSynthesize || synthesize.RequiredRole != team.RoleSupervisor {
		t.Fatalf("expected supervisor synthesize task, got %#v", synthesize)
	}
	if !slices.Equal(synthesize.DependsOn, []string{"api-verify", "ui-verify"}) {
		t.Fatalf("expected synthesize to depend on verifier tasks, got %#v", synthesize.DependsOn)
	}
	if !slices.Equal(synthesize.Reads, []string{"verify.api", "verify.ui"}) {
		t.Fatalf("expected synthesize to consume verifier outputs only, got %#v", synthesize.Reads)
	}
}

func TestEngineeringWorkflow_UsesExplicitDataflowAndVerifierGates(t *testing.T) {
	template, err := BuildEngineeringWorkflowTemplate(team.StartRequest{
		Input: map[string]any{
			"query": "ship the engineering workflow",
			"workItems": []any{
				map[string]any{"id": "api", "title": "implement API", "input": "Build the API contract"},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildEngineeringWorkflowTemplate() error = %v", err)
	}

	for _, task := range template.Tasks {
		switch task.Stage {
		case team.TaskStagePlan:
			if len(task.Writes) == 0 || !slices.Contains(task.Publish, team.OutputVisibilityBlackboard) {
				t.Fatalf("expected plan stage to publish explicit planning dataflow, got %#v", task)
			}
		case team.TaskStageImplement:
			if len(task.Reads) == 0 || len(task.Writes) == 0 || !slices.Contains(task.Publish, team.OutputVisibilityBlackboard) || !task.VerifierRequired {
				t.Fatalf("expected implement stage to use explicit dataflow and verifier gate, got %#v", task)
			}
		case team.TaskStageReview:
			if len(task.Reads) == 0 || len(task.Writes) == 0 || !slices.Contains(task.Publish, team.OutputVisibilityBlackboard) || !task.VerifierRequired {
				t.Fatalf("expected review stage to use explicit dataflow and verifier gate, got %#v", task)
			}
		case team.TaskStageVerify:
			if task.RequiredRole != team.RoleVerifier || len(task.Reads) == 0 || len(task.Writes) == 0 || !slices.Contains(task.Publish, team.OutputVisibilityBlackboard) {
				t.Fatalf("expected verify stage to run as verifier over explicit review outputs, got %#v", task)
			}
		case team.TaskStageSynthesize:
			if !task.VerifierRequired || len(task.Reads) == 0 || len(task.Writes) == 0 {
				t.Fatalf("expected synthesize stage to require verifier-approved inputs, got %#v", task)
			}
			if slices.Contains(task.Publish, team.OutputVisibilityBlackboard) {
				t.Fatalf("expected final synthesis to publish final output without reopening blackboard flow, got %#v", task.Publish)
			}
			for _, read := range task.Reads {
				if read != "verify.api" {
					t.Fatalf("expected synthesize to read only verify namespace outputs, got %#v", task.Reads)
				}
			}
		}
	}

	implement := taskByID(t, template.Tasks, "api")
	review := taskByID(t, template.Tasks, "api-review")
	verify := taskByID(t, template.Tasks, "api-verify")
	synthesize := taskByID(t, template.Tasks, engineeringWorkflowSynthTaskID)

	if !slices.Equal(implement.Reads, []string{"plan.api"}) || !slices.Equal(implement.Writes, []string{"implement.api"}) {
		t.Fatalf("expected implementation dataflow to be explicit, got %#v", implement)
	}
	if !slices.Equal(review.Reads, []string{"implement.api"}) || !slices.Equal(review.Writes, []string{"review.api"}) {
		t.Fatalf("expected review dataflow to be explicit, got %#v", review)
	}
	if !slices.Equal(verify.Reads, []string{"review.api"}) || !slices.Equal(verify.Writes, []string{"verify.api"}) {
		t.Fatalf("expected verify dataflow to be explicit, got %#v", verify)
	}
	if !slices.Equal(synthesize.DependsOn, []string{"api-verify"}) || !slices.Equal(synthesize.Reads, []string{"verify.api"}) {
		t.Fatalf("expected synthesize to be verifier-gated, got %#v", synthesize)
	}
	if slices.Contains(synthesize.Reads, "review.api") || slices.Contains(synthesize.Reads, "implement.api") {
		t.Fatalf("expected synthesize to reject hidden shared context, got %#v", synthesize.Reads)
	}
}

func taskByID(t *testing.T, tasks []TaskSpec, id string) TaskSpec {
	t.Helper()
	for _, task := range tasks {
		if task.ID == id {
			return task
		}
	}
	t.Fatalf("task %q not found in %#v", id, tasks)
	return TaskSpec{}
}
