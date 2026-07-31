package assertions_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/eval"
	"github.com/Viking602/venat/eval/assertions"
	"github.com/Viking602/venat/multiagent"
)

// appendEvent records a multi-agent transport event on the run's event store
// through the public Runner façade, so the multi-agent assertions observe it the
// same way they would observe a real scheduler's emissions.
func appendEvent(t *testing.T, h eval.Harness, runID string, typ api.EventType, payload map[string]any) {
	t.Helper()
	if err := h.Runner().AppendEvent(context.Background(), api.Event{
		RunID:   runID,
		Type:    typ,
		Payload: payload,
	}); err != nil {
		t.Fatalf("AppendEvent(%s) error = %v", typ, err)
	}
}

func TestAssertion_AgentInstanceSpawned_AtLeastMatcher(t *testing.T) {
	const runID = "run-spawned"
	run, h := runToTerminal(t, runID, "drive")
	appendEvent(t, h, runID, multiagent.EventAgentInstanceCreated, map[string]any{"className": "researcher"})
	appendEvent(t, h, runID, multiagent.EventAgentInstanceCreated, map[string]any{"className": "researcher"})
	appendEvent(t, h, runID, multiagent.EventAgentInstanceCreated, map[string]any{"className": "writer"})

	atLeastTwo := assertions.AgentInstanceSpawnedWith("researcher", assertions.AtLeast(2))
	if err := atLeastTwo.Check(context.Background(), run, h); err != nil {
		t.Fatalf("AtLeast(2) over two researchers should pass, got %v", err)
	}
	atLeastThree := assertions.AgentInstanceSpawnedWith("researcher", assertions.AtLeast(3))
	if err := atLeastThree.Check(context.Background(), run, h); err == nil {
		t.Fatalf("AtLeast(3) over two researchers should fail")
	}
	exactlyOne := assertions.AgentInstanceSpawnedWith("writer", assertions.Exactly(1))
	if err := exactlyOne.Check(context.Background(), run, h); err != nil {
		t.Fatalf("Exactly(1) over one writer should pass, got %v", err)
	}
	atMostZero := assertions.AgentInstanceSpawnedWith("writer", assertions.AtMost(0))
	if err := atMostZero.Check(context.Background(), run, h); err == nil {
		t.Fatalf("AtMost(0) over one writer should fail")
	}
	// No matcher requires at least one; an unspawned class fails.
	if err := (assertions.AgentInstanceSpawnedWith("ghost")).Check(context.Background(), run, h); err == nil {
		t.Fatalf("default at-least-one over an unspawned class should fail")
	}
}

func TestAssertion_SchedulerTookPath_SequentialMatch(t *testing.T) {
	const runID = "run-path"
	run, h := runToTerminal(t, runID, "drive")
	for _, class := range []string{"triage", "investigate", "report"} {
		appendEvent(t, h, runID, multiagent.EventDispatchEmitted, map[string]any{"className": class})
	}

	match := assertions.SchedulerTookPathOf("triage", "investigate", "report")
	if err := match.Check(context.Background(), run, h); err != nil {
		t.Fatalf("exact path should pass, got %v", err)
	}
	reordered := assertions.SchedulerTookPathOf("investigate", "triage", "report")
	if err := reordered.Check(context.Background(), run, h); err == nil {
		t.Fatalf("reordered path should fail (order is significant)")
	}
	truncated := assertions.SchedulerTookPathOf("triage", "investigate")
	if err := truncated.Check(context.Background(), run, h); err == nil {
		t.Fatalf("path of wrong length should fail")
	}
}

func TestAssertion_HandoffOccurred_DetectsTypedHandoff(t *testing.T) {
	const runID = "run-handoff"
	run, h := runToTerminal(t, runID, "drive")
	appendEvent(t, h, runID, multiagent.EventTypedHandoff, map[string]any{"from": "triage", "to": "specialist"})

	hit := assertions.HandoffOccurred{FromClass: "triage", ToClass: "specialist"}
	if err := hit.Check(context.Background(), run, h); err != nil {
		t.Fatalf("matching handoff should pass, got %v", err)
	}
	wrongTo := assertions.HandoffOccurred{FromClass: "triage", ToClass: "writer"}
	if err := wrongTo.Check(context.Background(), run, h); err == nil {
		t.Fatalf("handoff with non-matching destination should fail")
	}
	reversed := assertions.HandoffOccurred{FromClass: "specialist", ToClass: "triage"}
	if err := reversed.Check(context.Background(), run, h); err == nil {
		t.Fatalf("reversed handoff direction should fail")
	}
}

func TestAssertion_TeamTerminatedSuccessfully(t *testing.T) {
	run, h := runToTerminal(t, "run-team-ok", "drive")
	if err := (assertions.TeamTerminatedSuccessfully{}).Check(context.Background(), run, h); err != nil {
		t.Fatalf("completed run should pass, got %v", err)
	}

	failed := run
	failed.Status = api.RunStatusFailed
	if err := (assertions.TeamTerminatedSuccessfully{}).Check(context.Background(), failed, h); err == nil {
		t.Fatalf("failed run should not be reported as successful")
	}
}

func TestAssertion_NoNonIdempotentToolAutoRetried_DetectsViolation(t *testing.T) {
	const runID = "run-retry"
	run, h := runToTerminal(t, runID, "drive")
	// deploy (non-idempotent) is started twice: an auto-retry the loop must
	// never perform. search starts once and is fine.
	appendEvent(t, h, runID, api.EventActionAttemptStarted, map[string]any{"toolName": "deploy", "idempotencyKey": "k1"})
	appendEvent(t, h, runID, api.EventActionAttemptStarted, map[string]any{"toolName": "deploy", "idempotencyKey": "k2"})
	appendEvent(t, h, runID, api.EventActionAttemptStarted, map[string]any{"toolName": "search", "idempotencyKey": "s1"})

	guarded := assertions.NoNonIdempotentToolAutoRetried{NonIdempotentTools: []string{"deploy"}}
	if err := guarded.Check(context.Background(), run, h); err == nil {
		t.Fatalf("expected violation for twice-started deploy")
	}

	// search alone is single-attempt: scoping to it passes.
	onlySearch := assertions.NoNonIdempotentToolAutoRetried{NonIdempotentTools: []string{"search"}}
	if err := onlySearch.Check(context.Background(), run, h); err != nil {
		t.Fatalf("single-attempt search should pass, got %v", err)
	}
}

func TestAssertion_NoNonIdempotentToolAutoRetried_CleanRunPasses(t *testing.T) {
	const runID = "run-no-retry"
	run, h := runToTerminal(t, runID, "drive")
	appendEvent(t, h, runID, api.EventActionAttemptStarted, map[string]any{"toolName": "deploy", "idempotencyKey": "k1"})
	if err := (assertions.NoNonIdempotentToolAutoRetried{}).Check(context.Background(), run, h); err != nil {
		t.Fatalf("a single attempt per tool should pass under the default guard, got %v", err)
	}
}

// thresholdScorer is a fixed-score BPBScorer used to exercise the comparator.
type thresholdScorer struct {
	score float64
	err   error
}

func (s thresholdScorer) Score(ctx context.Context, run api.Run, harness eval.Harness) (float64, error) {
	return s.score, s.err
}

func TestAssertion_BPBLikeMetric_ScorerThresholdComparator(t *testing.T) {
	run, h := runToTerminal(t, "run-bpb", "drive")

	// Score above the threshold passes; at the threshold passes (inclusive);
	// below fails.
	above := assertions.BPBLikeMetric{Scorer: thresholdScorer{score: 0.9}, Threshold: 0.8}
	if err := above.Check(context.Background(), run, h); err != nil {
		t.Fatalf("score above threshold should pass, got %v", err)
	}
	atThreshold := assertions.BPBLikeMetric{Scorer: thresholdScorer{score: 0.8}, Threshold: 0.8}
	if err := atThreshold.Check(context.Background(), run, h); err != nil {
		t.Fatalf("score at threshold should pass (inclusive), got %v", err)
	}
	below := assertions.BPBLikeMetric{Scorer: thresholdScorer{score: 0.5}, Threshold: 0.8}
	if err := below.Check(context.Background(), run, h); err == nil {
		t.Fatalf("score below threshold should fail")
	}

	// A scorer error surfaces as a check failure.
	failing := assertions.BPBLikeMetric{Scorer: thresholdScorer{err: errors.New("boom")}, Threshold: 0.1}
	if err := failing.Check(context.Background(), run, h); err == nil {
		t.Fatalf("scorer error should fail the check")
	}
	// A nil scorer is a programming error, not a pass.
	if err := (assertions.BPBLikeMetric{Threshold: 0.1}).Check(context.Background(), run, h); err == nil {
		t.Fatalf("nil scorer should fail the check")
	}
}
