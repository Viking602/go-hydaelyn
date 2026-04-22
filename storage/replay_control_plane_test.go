package storage

import (
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/team"
)

// helper: build a well-formed control-plane chain for task IDs.
func controlPlaneChainEvents(tasks ...string) []Event {
	grants := make([]any, 0, len(tasks))
	for _, taskID := range tasks {
		grants = append(grants, map[string]any{"taskId": taskID})
	}
	events := []Event{
		{RunID: "team-1", TeamID: "team-1", Type: EventSupervisorObserved, Payload: map[string]any{
			"sequence": 1,
			"phase":    "research",
			"status":   "running",
		}},
		{RunID: "team-1", TeamID: "team-1", Type: EventSupervisorDecision, Payload: map[string]any{
			"kind":           string(team.DecisionGrantRun),
			"digestSequence": 1,
			"decisionNumber": 1,
			"grants":         grants,
		}},
	}
	for _, taskID := range tasks {
		events = append(events, Event{
			RunID: "team-1", TeamID: "team-1", TaskID: taskID,
			Type: EventTaskRunGranted,
			Payload: map[string]any{
				"taskId":         taskID,
				"digestSequence": 1,
				"decisionNumber": 1,
			},
		})
	}
	return events
}

func findMismatchByMessage(result []ReplayMismatch, fragment string) *ReplayMismatch {
	for i := range result {
		if strings.Contains(result[i].Message, fragment) {
			return &result[i]
		}
	}
	return nil
}

func TestValidateControlPlaneChain_HappyPath(t *testing.T) {
	t.Parallel()
	events := controlPlaneChainEvents("task-a")
	mismatches := validateControlPlaneChain(events)
	if len(mismatches) != 0 {
		t.Fatalf("expected clean chain to produce no mismatches, got %#v", mismatches)
	}
}

func TestValidateControlPlaneChain_GrantWithoutDecision(t *testing.T) {
	t.Parallel()
	events := []Event{
		{RunID: "team-1", TeamID: "team-1", Type: EventSupervisorObserved, Payload: map[string]any{"sequence": 1}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "task-x", Type: EventTaskRunGranted, Payload: map[string]any{
			"taskId":         "task-x",
			"digestSequence": 1,
			"decisionNumber": 1,
		}},
	}
	mismatches := validateControlPlaneChain(events)
	if findMismatchByMessage(mismatches, "no preceding SupervisorDecision") == nil {
		t.Fatalf("expected missing-decision mismatch, got %#v", mismatches)
	}
}

func TestValidateControlPlaneChain_DecisionWithoutObservation(t *testing.T) {
	t.Parallel()
	events := []Event{
		{RunID: "team-1", TeamID: "team-1", Type: EventSupervisorDecision, Payload: map[string]any{
			"kind":           string(team.DecisionGrantRun),
			"digestSequence": 7,
			"decisionNumber": 1,
		}},
	}
	mismatches := validateControlPlaneChain(events)
	if findMismatchByMessage(mismatches, "no preceding SupervisorObserved") == nil {
		t.Fatalf("expected missing-observation mismatch, got %#v", mismatches)
	}
}

func TestValidateControlPlaneChain_GrantForUnlistedTask(t *testing.T) {
	t.Parallel()
	events := controlPlaneChainEvents("task-a")
	// Append a grant for a task that was never listed in any decision.
	events = append(events, Event{
		RunID: "team-1", TeamID: "team-1", TaskID: "task-rogue",
		Type: EventTaskRunGranted,
		Payload: map[string]any{
			"taskId":         "task-rogue",
			"digestSequence": 1,
			"decisionNumber": 1,
		},
	})
	mismatches := validateControlPlaneChain(events)
	if findMismatchByMessage(mismatches, "was not declared in SupervisorDecision") == nil {
		t.Fatalf("expected unlisted-task mismatch, got %#v", mismatches)
	}
}

func TestValidateSynthesisChain_CommittedWithoutPacket(t *testing.T) {
	t.Parallel()
	events := []Event{
		{RunID: "team-1", TeamID: "team-1", Type: EventSupervisorObserved, Payload: map[string]any{"sequence": 1}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "synth-1", Type: EventSynthesisCommitted, Payload: map[string]any{"summary": "oops"}},
	}
	mismatches := validateSynthesisChain(events, TeamState{})
	if findMismatchByMessage(mismatches, "no preceding SynthesisPacketBuilt") == nil {
		t.Fatalf("expected missing-packet mismatch, got %#v", mismatches)
	}
}

func TestValidateSynthesisChain_LegacyModeSkipsPacketRequirement(t *testing.T) {
	t.Parallel()
	// A run without any SupervisorObserved event (Legacy control mode)
	// must not be penalized for missing a SynthesisPacketBuilt — the
	// supervisor never ran, so no packet could ever exist.
	events := []Event{
		{RunID: "team-1", TeamID: "team-1", TaskID: "synth-1", Type: EventSynthesisCommitted, Payload: map[string]any{"summary": "legacy"}},
	}
	mismatches := validateSynthesisChain(events, TeamState{})
	if len(mismatches) != 0 {
		t.Fatalf("legacy-mode run must not trigger packet invariant, got %#v", mismatches)
	}
}

func TestValidateSynthesisChain_HappyPathNoCitations(t *testing.T) {
	t.Parallel()
	events := []Event{
		{RunID: "team-1", TeamID: "team-1", Type: EventSynthesisPacketBuilt, Payload: map[string]any{
			"packet": map[string]any{
				"findingIds": []string{"f1"},
				"claimIds":   []string{"c1"},
			},
		}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "synth-1", Type: EventSynthesisCommitted, Payload: map[string]any{"summary": "ok"}},
	}
	mismatches := validateSynthesisChain(events, TeamState{})
	if len(mismatches) != 0 {
		t.Fatalf("expected clean chain, got %#v", mismatches)
	}
}

func TestValidateSynthesisChain_CitationOutsidePacket(t *testing.T) {
	t.Parallel()
	events := []Event{
		{RunID: "team-1", TeamID: "team-1", Type: EventSupervisorObserved, Payload: map[string]any{"sequence": 1}},
		{RunID: "team-1", TeamID: "team-1", Type: EventSynthesisPacketBuilt, Payload: map[string]any{
			"packet": map[string]any{
				"findingIds": []string{"f1"},
			},
		}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "synth-1", Type: EventSynthesisCommitted, Payload: map[string]any{"summary": "ok"}},
	}
	state := TeamState{
		Result: &team.Result{
			Structured: map[string]any{
				team.ReportKey: map[string]any{
					"kind":   string(team.ReportKindSynthesis),
					"answer": "final",
					"citations": []any{
						map[string]any{"findingId": "f1"},
						map[string]any{"findingId": "f-ghost"},
					},
				},
			},
		},
	}
	mismatches := validateSynthesisChain(events, state)
	if findMismatchByMessage(mismatches, "f-ghost") == nil {
		t.Fatalf("expected ghost citation mismatch, got %#v", mismatches)
	}
	// f1 is legitimate — make sure we did not falsely flag it.
	if findMismatchByMessage(mismatches, "citation f1 was not") != nil {
		t.Fatalf("unexpected mismatch for legit citation: %#v", mismatches)
	}
}

func TestValidateSynthesisChain_AllCitationsInPacket(t *testing.T) {
	t.Parallel()
	events := []Event{
		{RunID: "team-1", TeamID: "team-1", Type: EventSupervisorObserved, Payload: map[string]any{"sequence": 1}},
		{RunID: "team-1", TeamID: "team-1", Type: EventSynthesisPacketBuilt, Payload: map[string]any{
			"packet": map[string]any{
				"findingIds":  []string{"f1", "f2"},
				"exchangeIds": []string{"ex-1"},
			},
		}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "synth-1", Type: EventSynthesisCommitted, Payload: map[string]any{"summary": "ok"}},
	}
	state := TeamState{
		Result: &team.Result{
			Structured: map[string]any{
				team.ReportKey: map[string]any{
					"kind":   string(team.ReportKindSynthesis),
					"answer": "final",
					"citations": []any{
						map[string]any{"findingId": "f1"},
						map[string]any{"exchangeId": "ex-1"},
					},
				},
			},
		},
	}
	mismatches := validateSynthesisChain(events, state)
	if len(mismatches) != 0 {
		t.Fatalf("expected no mismatches, got %#v", mismatches)
	}
}
