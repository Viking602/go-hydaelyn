package panel

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/internal/blackboard"
	"github.com/Viking602/go-hydaelyn/team"
)

func TestPanelPlansClaimedTodosAndCrossReviewFlow(t *testing.T) {
	pattern := New()
	state, err := pattern.Start(context.Background(), team.StartRequest{
		Pattern:           pattern.Name(),
		TeamID:            "team-1",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"security", "frontend"},
		Input: map[string]any{
			"query": "launch auth feature",
			"experts": []any{
				map[string]any{"profile": "security", "domains": []any{"security"}, "capabilities": []any{"threat_model"}},
				map[string]any{"profile": "frontend", "domains": []any{"frontend"}, "capabilities": []any{"browser"}},
			},
			"todos": []any{
				map[string]any{"id": "security-review", "title": "review auth threat model", "domain": "security", "requiredCapabilities": []any{"threat_model"}, "priority": "high"},
				map[string]any{"id": "ui-review", "title": "review login UI", "domain": "frontend", "requiredCapabilities": []any{"browser"}},
			},
			"requireVerification": true,
		},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if state.Pattern != "panel" || state.InteractionMode != team.InteractionModePanel {
		t.Fatalf("expected panel interaction state, got %#v", state)
	}
	if state.TaskBoard == nil || len(state.TaskBoard.Plan.Items) != 2 {
		t.Fatalf("expected task board with two todos, got %#v", state.TaskBoard)
	}
	if len(state.Tasks) != 2 {
		t.Fatalf("expected two claimed research tasks, got %#v", state.Tasks)
	}
	for _, task := range state.Tasks {
		if task.ExpectedReportKind != team.ReportKindResearch || task.TodoID == "" {
			t.Fatalf("expected panel research task to require typed report and todo id, got %#v", task)
		}
		if len(task.Reads) != 0 {
			t.Fatalf("expected new panel tasks to use selectors rather than legacy reads, got %#v", task.Reads)
		}
	}
	if state.Tasks[0].AssigneeAgentID != "worker-1" || state.Tasks[1].AssigneeAgentID != "worker-2" {
		t.Fatalf("expected capability-based claims, got %#v", state.Tasks)
	}

	for idx := range state.Tasks {
		state.Tasks[idx].Status = team.TaskStatusCompleted
		state.Tasks[idx].Result = &team.Result{Summary: state.Tasks[idx].Title + " done"}
	}
	reviewState, err := pattern.Advance(context.Background(), state)
	if err != nil {
		t.Fatalf("Advance() review error = %v", err)
	}
	if reviewState.Phase != team.PhaseVerify {
		t.Fatalf("expected panel to enter verify phase after research, got %s", reviewState.Phase)
	}
	if len(reviewState.Tasks) != 4 {
		t.Fatalf("expected review tasks to be appended, got %#v", reviewState.Tasks)
	}
	for _, task := range reviewState.Tasks[2:] {
		if task.Kind != team.TaskKindVerify || task.Stage != team.TaskStageReview {
			t.Fatalf("expected cross-review verifier task, got %#v", task)
		}
		if task.ExpectedReportKind != team.ReportKindVerification {
			t.Fatalf("expected review task to require verification report, got %#v", task)
		}
		if len(task.ReadSelectors) != 1 || !task.ReadSelectors[0].Required {
			t.Fatalf("expected review task selector, got %#v", task.ReadSelectors)
		}
		if task.ReadSelectors[0].TaskIDs[0] != baseTodoID(task.TodoID) {
			t.Fatalf("expected review to read source todo output, got %#v", task.ReadSelectors)
		}
	}

	for idx := 2; idx < len(reviewState.Tasks); idx++ {
		reviewState.Tasks[idx].Status = team.TaskStatusCompleted
		reviewState.Tasks[idx].Result = &team.Result{
			Summary:    "supported",
			Confidence: 0.9,
			Structured: map[string]any{
				team.ReportKey: map[string]any{
					"kind":       string(team.ReportKindVerification),
					"status":     string(team.VerificationStatusSupported),
					"confidence": 0.9,
				},
			},
		}
	}
	synthState, err := pattern.Advance(context.Background(), reviewState)
	if err != nil {
		t.Fatalf("Advance() synth error = %v", err)
	}
	synth := synthState.Tasks[len(synthState.Tasks)-1]
	if synth.Kind != team.TaskKindSynthesize || synth.ExpectedReportKind != team.ReportKindSynthesis {
		t.Fatalf("expected typed synthesis task, got %#v", synth)
	}
	if len(synth.ReadSelectors) != 1 || !synth.ReadSelectors[0].RequireVerified {
		t.Fatalf("expected synthesis to read verified findings via selector, got %#v", synth.ReadSelectors)
	}
	if len(synth.Reads) != 0 {
		t.Fatalf("expected synthesis to avoid legacy reads, got %#v", synth.Reads)
	}
}

func TestPanelSynthesisAddsFullEvidenceTrail(t *testing.T) {
	state := team.RunState{
		TaskBoard: &team.TaskBoard{Plan: team.TodoPlan{Items: []team.TodoItem{{
			ID:             "security-review",
			Title:          "review auth threat model",
			Status:         team.TodoStatusVerified,
			PrimaryAgentID: "worker-1",
		}}}},
		Workers: []team.AgentInstance{{ID: "worker-1", ProfileName: "security"}},
		Tasks: []team.Task{{
			ID:     "task-synthesize",
			Kind:   team.TaskKindSynthesize,
			Stage:  team.TaskStageSynthesize,
			Status: team.TaskStatusCompleted,
			Result: &team.Result{Summary: "final answer", Structured: map[string]any{
				team.ReportKey: map[string]any{"kind": string(team.ReportKindSynthesis), "answer": "final answer"},
			}},
		}},
		Blackboard: &blackboard.State{
			Claims: []blackboard.Claim{
				{ID: "claim-1", Summary: "supported claim"},
				{ID: "claim-2", Summary: "weak claim"},
			},
			Findings: []blackboard.Finding{{ID: "finding-1", Summary: "supported finding", ClaimIDs: []string{"claim-1"}, Confidence: 0.9}},
			Verifications: []blackboard.VerificationResult{
				{ClaimID: "claim-1", Status: blackboard.VerificationStatusSupported, Confidence: 0.9, EvidenceIDs: []string{"ev-1"}},
				{ClaimID: "claim-2", Status: blackboard.VerificationStatusInsufficient, Confidence: 0.6},
			},
			Evidence: []blackboard.Evidence{{ID: "ev-1", Snippet: "source evidence"}},
		},
	}

	result := synthesizedResult(state)
	panel, ok := result.Structured["panel"].(map[string]any)
	if !ok {
		t.Fatalf("expected final result to include panel evidence trail, got %#v", result.Structured)
	}
	if adopted, _ := panel["adoptedFindings"].([]map[string]any); len(adopted) != 1 {
		t.Fatalf("expected one adopted finding, got %#v", panel["adoptedFindings"])
	}
	if excluded, _ := panel["excludedClaims"].([]map[string]any); len(excluded) != 1 {
		t.Fatalf("expected one excluded claim, got %#v", panel["excludedClaims"])
	}
}

func TestPanelCreatesConfiguredReviewerTasks(t *testing.T) {
	pattern := New()
	state, err := pattern.Start(context.Background(), team.StartRequest{
		Pattern:           pattern.Name(),
		TeamID:            "team-1",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"primary", "reviewer-a", "reviewer-b"},
		Input: map[string]any{
			"query": "ship risky auth change",
			"todos": []any{map[string]any{
				"id":    "security-review",
				"title": "review auth threat model",
				"verificationPolicy": map[string]any{
					"required":  true,
					"reviewers": 2,
				},
			}},
		},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	state.Tasks[0].Status = team.TaskStatusCompleted
	state.Tasks[0].Result = &team.Result{Summary: "done"}

	next, err := pattern.Advance(context.Background(), state)
	if err != nil {
		t.Fatalf("Advance() error = %v", err)
	}
	if got := len(next.Tasks); got != 3 {
		t.Fatalf("expected one research task and two review tasks, got %#v", next.Tasks)
	}
	reviewers := next.TaskBoard.Plan.Items[0].ReviewerAgentIDs
	if len(reviewers) != 2 || reviewers[0] != "worker-2" || reviewers[1] != "worker-3" {
		t.Fatalf("expected configured reviewers to be recorded, got %#v", next.TaskBoard.Plan.Items[0])
	}
	if next.Tasks[1].ID != "security-review-review" || next.Tasks[2].ID != "security-review-review-2" {
		t.Fatalf("expected stable review task ids, got %#v", next.Tasks[1:])
	}
}

func TestPanelDefaultsTodoDomainToProfileName(t *testing.T) {
	pattern := New()
	state, err := pattern.Start(context.Background(), team.StartRequest{
		Pattern:           pattern.Name(),
		TeamID:            "team-1",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"security", "frontend"},
		Input: map[string]any{
			"query": "launch auth feature",
			"todos": []any{
				map[string]any{"id": "security-review", "title": "review auth threat model", "domain": "security"},
				map[string]any{"id": "ui-review", "title": "review login UI", "domain": "frontend"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if state.Tasks[0].AssigneeAgentID != "worker-1" || state.Tasks[1].AssigneeAgentID != "worker-2" {
		t.Fatalf("expected profile-name domains to route todos to matching experts, got %#v", state.Tasks)
	}
}
