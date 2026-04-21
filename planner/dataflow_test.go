package planner

import (
	"slices"
	"testing"

	"github.com/Viking602/go-hydaelyn/team"
)

func TestPlannerDataflowReadsHaveCorrespondingWrites(t *testing.T) {
	template, err := BuildEngineeringWorkflowTemplate(team.StartRequest{
		Input: map[string]any{
			"query": "ship verified workflow",
			"workItems": []any{
				map[string]any{"id": "api", "title": "implement API", "input": "Build API"},
				map[string]any{"id": "ui", "title": "implement UI", "input": "Build UI"},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildEngineeringWorkflowTemplate() error = %v", err)
	}

	writers := map[string]string{}
	for _, task := range template.Tasks {
		for _, write := range task.Writes {
			if prior, exists := writers[write]; exists {
				t.Fatalf("write key %q published by both %q and %q", write, prior, task.ID)
			}
			writers[write] = task.ID
		}
	}

	for _, task := range template.Tasks {
		for _, read := range task.Reads {
			if _, ok := writers[read]; !ok {
				t.Fatalf("task %q reads %q without a corresponding writer in %#v", task.ID, read, template.Tasks)
			}
		}
	}
}

func TestPlannerPublishCompletenessKeepsIntermediateAndFinalOutputsVisible(t *testing.T) {
	template, err := BuildEngineeringWorkflowTemplate(team.StartRequest{
		Input: map[string]any{
			"query":     "ship verified workflow",
			"workItems": []any{map[string]any{"id": "api", "title": "implement API", "input": "Build API"}},
		},
	})
	if err != nil {
		t.Fatalf("BuildEngineeringWorkflowTemplate() error = %v", err)
	}

	for _, task := range template.Tasks {
		if len(task.Writes) == 0 {
			continue
		}
		switch task.Stage {
		case team.TaskStageSynthesize:
			if !slices.Contains(task.Publish, team.OutputVisibilityShared) {
				t.Fatalf("expected final synthesis to publish shared output, got %#v", task)
			}
		case team.TaskStagePlan, team.TaskStageImplement, team.TaskStageReview, team.TaskStageVerify:
			if !slices.Contains(task.Publish, team.OutputVisibilityShared) || !slices.Contains(task.Publish, team.OutputVisibilityBlackboard) {
				t.Fatalf("expected stage %q writes to publish shared + blackboard visibility, got %#v", task.Stage, task)
			}
		}
	}
}
