package deepsearch

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/blackboard"
	"github.com/Viking602/go-hydaelyn/team"
)

func TestPatternCreatesResearchTasksVerificationAndExplicitSynthesisPhase(t *testing.T) {
	pattern := New()
	state, err := pattern.Start(context.Background(), team.StartRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"research-a", "research-b"},
		Input: map[string]any{
			"query":               "agent frameworks",
			"subqueries":          []string{"architecture", "verification"},
			"requireVerification": true,
		},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if state.Phase != team.PhaseResearch {
		t.Fatalf("expected research phase, got %s", state.Phase)
	}
	if len(state.Tasks) != 2 {
		t.Fatalf("expected 2 research tasks, got %d", len(state.Tasks))
	}
	for idx := range state.Tasks {
		state.Tasks[idx].Status = team.TaskStatusCompleted
		state.Tasks[idx].Result = &team.Result{
			Summary: state.Tasks[idx].Input,
		}
	}
	next, err := pattern.Advance(context.Background(), state)
	if err != nil {
		t.Fatalf("Advance() error = %v", err)
	}
	if next.Phase != team.PhaseVerify {
		t.Fatalf("expected verification phase, got %s", next.Phase)
	}
	if len(next.Tasks) != 4 {
		t.Fatalf("expected research + verifier tasks to be present, got %d", len(next.Tasks))
	}
	for _, task := range next.Tasks[2:] {
		if task.Kind != team.TaskKindVerify {
			t.Fatalf("expected verify tasks, got %#v", next.Tasks)
		}
		if len(task.Reads) != 1 || len(task.Writes) != 1 {
			t.Fatalf("expected verify task reads/writes to be explicit, got %#v", task)
		}
	}
	for idx := 2; idx < len(next.Tasks); idx++ {
		next.Tasks[idx].Status = team.TaskStatusCompleted
		next.Tasks[idx].Result = &team.Result{
			Summary: "supported",
		}
	}
	synthState := next
	synthState.Blackboard = &blackboard.State{
		Findings: []blackboard.Finding{
			{ID: "finding-1", TaskID: "task-1", Summary: "architecture", ClaimIDs: []string{"claim-1"}, Confidence: 0.9},
		},
		Verifications: []blackboard.VerificationResult{
			{ClaimID: "claim-1", Status: blackboard.VerificationStatusSupported, Confidence: 0.9},
		},
	}
	synth, err := pattern.Advance(context.Background(), synthState)
	if err != nil {
		t.Fatalf("Advance() synth error = %v", err)
	}
	if synth.Phase != team.PhaseSynthesize || synth.Status != team.StatusRunning {
		t.Fatalf("expected explicit synthesize phase, got %#v", synth)
	}
	if len(synth.Tasks) != 5 {
		t.Fatalf("expected synth task to be appended, got %#v", synth.Tasks)
	}
	last := synth.Tasks[len(synth.Tasks)-1]
	if last.Kind != team.TaskKindSynthesize {
		t.Fatalf("expected synth task, got %#v", last)
	}
	if len(last.Reads) != 1 || last.Reads[0] != "supported_findings" {
		t.Fatalf("expected synth task to read supported_findings, got %#v", last)
	}
	last.Status = team.TaskStatusCompleted
	last.Result = &team.Result{Summary: "architecture", Findings: []team.Finding{{Summary: "architecture", Confidence: 0.9}}}
	synth.Tasks[len(synth.Tasks)-1] = last
	completed, err := pattern.Advance(context.Background(), synth)
	if err != nil {
		t.Fatalf("Advance() completion error = %v", err)
	}
	if completed.Status != team.StatusCompleted || completed.Result == nil {
		t.Fatalf("expected completed synthesized result, got %#v", completed)
	}
	if completed.Result.Summary != "architecture" {
		t.Fatalf("expected synth result to become team result, got %#v", completed.Result)
	}
}
