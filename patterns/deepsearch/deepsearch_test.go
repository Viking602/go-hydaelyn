package deepsearch

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/blackboard"
	"github.com/Viking602/go-hydaelyn/team"
)

func TestDeepsearchSingleBranchExecution(t *testing.T) {
	pattern := New()
	state, err := pattern.Start(context.Background(), team.StartRequest{
		Pattern:           pattern.Name(),
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input:             map[string]any{"query": "single branch"},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if len(state.Tasks) != 1 || state.Tasks[0].Input != "single branch" {
		t.Fatalf("expected fallback query branch, got %#v", state.Tasks)
	}
	state.Tasks[0].Status = team.TaskStatusCompleted
	state.Tasks[0].Result = &team.Result{Summary: "single branch result", Confidence: 0.7}

	next, err := pattern.Advance(context.Background(), state)
	if err != nil {
		t.Fatalf("Advance() error = %v", err)
	}
	if next.Phase != team.PhaseSynthesize {
		t.Fatalf("expected synthesize phase without verification, got %#v", next.Phase)
	}
	synth := next.Tasks[len(next.Tasks)-1]
	if synth.Kind != team.TaskKindSynthesize || !slices.Equal(synth.Reads, []string{"research.task-1"}) {
		t.Fatalf("expected synth task to consume research output, got %#v", synth)
	}
	if !slices.Equal(synth.DependsOn, []string{"task-1"}) {
		t.Fatalf("expected synth task to depend on research task, got %#v", synth.DependsOn)
	}
}

func TestDeepsearchMultiBranchParallelResearch(t *testing.T) {
	pattern := New()
	state, err := pattern.Start(context.Background(), team.StartRequest{
		Pattern:           pattern.Name(),
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"research-a", "research-b"},
		Input: map[string]any{
			"query":      "parallel research",
			"subqueries": []string{"architecture", "verification", "tooling"},
		},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if len(state.Tasks) != 3 {
		t.Fatalf("expected 3 research tasks, got %#v", state.Tasks)
	}
	gotAssignees := []string{state.Tasks[0].AssigneeAgentID, state.Tasks[1].AssigneeAgentID, state.Tasks[2].AssigneeAgentID}
	if !slices.Equal(gotAssignees, []string{"worker-1", "worker-2", "worker-1"}) {
		t.Fatalf("expected round-robin worker assignment, got %#v", gotAssignees)
	}
	for _, task := range state.Tasks {
		if task.Kind != team.TaskKindResearch || len(task.Writes) != 1 || task.Writes[0] != researchWriteKey(task.ID) {
			t.Fatalf("expected explicit research branch writes, got %#v", task)
		}
	}
}

func TestVerifierGateDeepsearchRequiresVerifiedInputs(t *testing.T) {
	pattern := New()
	state, err := pattern.Start(context.Background(), team.StartRequest{
		Pattern:           pattern.Name(),
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"research-a", "research-b"},
		Input: map[string]any{
			"query":               "verified research",
			"subqueries":          []string{"architecture", "verification"},
			"requireVerification": true,
		},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	for idx := range state.Tasks {
		state.Tasks[idx].Status = team.TaskStatusCompleted
		state.Tasks[idx].Result = &team.Result{Summary: state.Tasks[idx].Input}
	}

	verified, err := pattern.Advance(context.Background(), state)
	if err != nil {
		t.Fatalf("Advance() verify error = %v", err)
	}
	if verified.Phase != team.PhaseVerify {
		t.Fatalf("expected verify phase, got %#v", verified.Phase)
	}
	for _, task := range verified.Tasks[2:] {
		if task.Kind != team.TaskKindVerify || !slices.Equal(task.Reads, []string{researchWriteKey(baseTaskID(task.ID))}) {
			t.Fatalf("expected verify tasks to read research outputs, got %#v", task)
		}
	}
	for idx := 2; idx < len(verified.Tasks); idx++ {
		verified.Tasks[idx].Status = team.TaskStatusCompleted
		verified.Tasks[idx].Result = &team.Result{Summary: "supported"}
	}

	synthState, err := pattern.Advance(context.Background(), verified)
	if err != nil {
		t.Fatalf("Advance() synth error = %v", err)
	}
	synth := synthState.Tasks[len(synthState.Tasks)-1]
	if !slices.Equal(synth.Reads, []string{"supported_findings"}) {
		t.Fatalf("expected synth to read verifier gate output, got %#v", synth.Reads)
	}
	if !slices.Equal(synth.DependsOn, []string{"task-1-verify", "task-2-verify"}) {
		t.Fatalf("expected synth to depend on verifier tasks, got %#v", synth.DependsOn)
	}
}

func TestDeepsearchSynthesisCoverageTracking(t *testing.T) {
	state := team.RunState{
		RequireVerification: true,
		Tasks: []team.Task{
			{ID: "task-1", Kind: team.TaskKindResearch, Status: team.TaskStatusCompleted, Result: &team.Result{Summary: "architecture", Confidence: 0.6}},
			{ID: "task-2", Kind: team.TaskKindResearch, Status: team.TaskStatusCompleted, Result: &team.Result{Summary: "verification", Confidence: 0.5}},
			{ID: "task-synthesize", Kind: team.TaskKindSynthesize, Status: team.TaskStatusCompleted, Result: &team.Result{}},
		},
		Blackboard: &blackboard.State{
			Findings: []blackboard.Finding{
				{ID: "finding-1", TaskID: "task-1", Summary: "architecture", ClaimIDs: []string{"claim-1"}, Confidence: 0.9},
				{ID: "finding-2", TaskID: "task-2", Summary: "verification", ClaimIDs: []string{"claim-2"}, Confidence: 0.7},
			},
			Verifications: []blackboard.VerificationResult{
				{ClaimID: "claim-1", Status: blackboard.VerificationStatusSupported, Confidence: 0.9, EvidenceIDs: []string{"evidence-1"}},
			},
		},
	}

	result := synthesizedResult(state)
	if len(result.Findings) != 1 {
		t.Fatalf("expected synthesis to include only supported findings, got %#v", result.Findings)
	}
	if result.Findings[0].Summary != "architecture" || result.Confidence != 0.9 {
		t.Fatalf("expected supported finding coverage to drive result, got %#v", result)
	}
}

func baseTaskID(taskID string) string {
	for _, suffix := range []string{"-verify"} {
		if trimmed, ok := strings.CutSuffix(taskID, suffix); ok {
			return trimmed
		}
	}
	return taskID
}
