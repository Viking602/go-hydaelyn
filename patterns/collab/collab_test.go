package collab

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/team"
)

func TestCollabPattern_StartBuildsPlannedCollaboration(t *testing.T) {
	pattern := New()
	request := team.StartRequest{
		Pattern:           pattern.Name(),
		TeamID:            "team-1",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"engineer-a", "engineer-b"},
		Input: map[string]any{
			"query": "ship collaboration workflow",
			"branches": []any{
				map[string]any{"id": "impl-api", "title": "implement API", "input": "build the API contract", "verifierRequired": true},
				map[string]any{"id": "impl-ui", "title": "implement UI", "input": "build the UI flow"},
			},
		},
	}

	template, err := pattern.PlanTemplate(request)
	if err != nil {
		t.Fatalf("PlanTemplate() error = %v", err)
	}
	if template.Name != pattern.Name() {
		t.Fatalf("expected template name %q, got %q", pattern.Name(), template.Name)
	}
	if template.Goal != "ship collaboration workflow" {
		t.Fatalf("expected template goal to carry query, got %#v", template.Goal)
	}
	if !template.VerificationPolicy.Required {
		t.Fatalf("expected collaboration template to require verification when any branch requires it, got %#v", template.VerificationPolicy)
	}
	if len(template.Tasks) != 2 {
		t.Fatalf("expected 2 planned implement tasks, got %#v", template.Tasks)
	}
	if template.Tasks[0].Stage != team.TaskStageImplement || template.Tasks[0].Namespace != "impl.impl-api" || !template.Tasks[0].VerifierRequired {
		t.Fatalf("expected first planned task to carry collaboration metadata, got %#v", template.Tasks[0])
	}
	if len(template.Tasks[0].Writes) != 1 || template.Tasks[0].Writes[0] != "implement.impl-api" {
		t.Fatalf("expected implement write key, got %#v", template.Tasks[0].Writes)
	}

	state, err := pattern.Start(context.Background(), request)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if state.Pattern != pattern.Name() || state.Phase != team.PhaseResearch || state.Status != team.StatusRunning {
		t.Fatalf("expected running collaboration state, got %#v", state)
	}
	if !state.RequireVerification {
		t.Fatalf("expected state to require verification, got %#v", state.RequireVerification)
	}
	if len(state.Tasks) != 2 {
		t.Fatalf("expected two implement tasks, got %#v", state.Tasks)
	}
	first := state.Tasks[0]
	if first.Stage != team.TaskStageImplement || first.Kind != team.TaskKindResearch {
		t.Fatalf("expected implement research task, got %#v", first)
	}
	if first.Namespace != "impl.impl-api" || !first.VerifierRequired {
		t.Fatalf("expected implement task collaboration metadata, got %#v", first)
	}
	if len(first.Writes) != 1 || first.Writes[0] != "implement.impl-api" {
		t.Fatalf("expected implement write key, got %#v", first.Writes)
	}
	if first.AssigneeAgentID != "worker-1" || state.Tasks[1].AssigneeAgentID != "worker-2" {
		t.Fatalf("expected round-robin worker assignment, got %#v", state.Tasks)
	}
}

func TestCollabPattern_AdvanceCreatesVerifierAndSynthesis(t *testing.T) {
	pattern := New()
	state, err := pattern.Start(context.Background(), team.StartRequest{
		Pattern:           pattern.Name(),
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"engineer-a", "engineer-b"},
		Input: map[string]any{
			"query":               "ship collaboration workflow",
			"requireVerification": true,
			"branches": []any{
				map[string]any{"id": "impl-api", "title": "implement API", "input": "build the API contract"},
				map[string]any{"id": "impl-ui", "title": "implement UI", "input": "build the UI flow"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	for idx := range state.Tasks {
		state.Tasks[idx].Status = team.TaskStatusCompleted
		state.Tasks[idx].Result = &team.Result{Summary: state.Tasks[idx].Title + " done"}
	}

	reviewState, err := pattern.Advance(context.Background(), state)
	if err != nil {
		t.Fatalf("Advance() review error = %v", err)
	}
	if reviewState.Phase != team.PhaseResearch {
		t.Fatalf("expected review creation to stay in research runtime phase, got %#v", reviewState.Phase)
	}
	if len(reviewState.Tasks) != 4 {
		t.Fatalf("expected review tasks to be appended, got %#v", reviewState.Tasks)
	}
	for _, task := range reviewState.Tasks[2:] {
		if task.Stage != team.TaskStageReview || task.Namespace != "review."+baseTaskID(task.ID) {
			t.Fatalf("expected review metadata, got %#v", task)
		}
		if len(task.Reads) != 1 || task.Reads[0] != "implement."+baseTaskID(task.ID) {
			t.Fatalf("expected review task to read implementation output, got %#v", task)
		}
		if len(task.Writes) != 1 || task.Writes[0] != "review."+baseTaskID(task.ID) {
			t.Fatalf("expected review task to publish review output, got %#v", task)
		}
	}

	for idx := 2; idx < len(reviewState.Tasks); idx++ {
		reviewState.Tasks[idx].Status = team.TaskStatusCompleted
		reviewState.Tasks[idx].Result = &team.Result{Summary: reviewState.Tasks[idx].Title + " approved"}
	}

	verifyState, err := pattern.Advance(context.Background(), reviewState)
	if err != nil {
		t.Fatalf("Advance() verify error = %v", err)
	}
	if verifyState.Phase != team.PhaseVerify {
		t.Fatalf("expected verify runtime phase, got %#v", verifyState.Phase)
	}
	if len(verifyState.Tasks) != 6 {
		t.Fatalf("expected verify tasks to be appended, got %#v", verifyState.Tasks)
	}
	for _, task := range verifyState.Tasks[4:] {
		if task.Stage != team.TaskStageVerify || task.Kind != team.TaskKindVerify || task.Namespace != "verify."+baseTaskID(task.ID) {
			t.Fatalf("expected verify metadata, got %#v", task)
		}
		if len(task.Reads) != 1 || task.Reads[0] != "review."+baseTaskID(task.ID) {
			t.Fatalf("expected verify task to read review output, got %#v", task)
		}
		if len(task.Writes) != 1 || task.Writes[0] != "verify."+baseTaskID(task.ID) {
			t.Fatalf("expected verify task to write verifier output, got %#v", task)
		}
	}

	for idx := 4; idx < len(verifyState.Tasks); idx++ {
		verifyState.Tasks[idx].Status = team.TaskStatusCompleted
		verifyState.Tasks[idx].Result = &team.Result{Summary: verifyState.Tasks[idx].Title + " supported", Confidence: 0.9}
	}

	synthState, err := pattern.Advance(context.Background(), verifyState)
	if err != nil {
		t.Fatalf("Advance() synth error = %v", err)
	}
	if synthState.Phase != team.PhaseSynthesize {
		t.Fatalf("expected synthesize phase, got %#v", synthState.Phase)
	}
	if len(synthState.Tasks) != 7 {
		t.Fatalf("expected synth task to be appended, got %#v", synthState.Tasks)
	}
	synth := synthState.Tasks[len(synthState.Tasks)-1]
	if synth.Stage != team.TaskStageSynthesize || synth.Kind != team.TaskKindSynthesize || !synth.VerifierRequired {
		t.Fatalf("expected guarded synth task, got %#v", synth)
	}
	if len(synth.Reads) != 2 || synth.Reads[0] != "verify.impl-api" || synth.Reads[1] != "verify.impl-ui" {
		t.Fatalf("expected synth task to read verifier outputs, got %#v", synth.Reads)
	}

	synth.Status = team.TaskStatusCompleted
	synth.Result = &team.Result{Summary: "ship collaboration workflow"}
	synthState.Tasks[len(synthState.Tasks)-1] = synth

	completed, err := pattern.Advance(context.Background(), synthState)
	if err != nil {
		t.Fatalf("Advance() completion error = %v", err)
	}
	if completed.Status != team.StatusCompleted || completed.Result == nil {
		t.Fatalf("expected completed collaboration result, got %#v", completed)
	}
	if completed.Result.Summary != "ship collaboration workflow" {
		t.Fatalf("expected synth summary to become final result, got %#v", completed.Result)
	}
	if len(completed.Result.Findings) != 2 {
		t.Fatalf("expected verifier findings to be attached to final result, got %#v", completed.Result)
	}
}
