package assertions_test

import (
	"context"
	"testing"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/eval"
	"github.com/Viking602/venat/eval/assertions"
)

// saveDecisionTask attaches policy decisions to a fresh task on the run so the
// policy assertions observe them through ListTasks.
func saveDecisionTask(t *testing.T, h eval.Harness, runID, taskID string, decisions ...api.PolicyDecision) {
	t.Helper()
	if err := h.Runner().SaveTask(context.Background(), api.Task{
		ID:              taskID,
		RunID:           runID,
		Type:            api.TaskTypeWorker,
		Status:          api.TaskStatusCompleted,
		PolicyDecisions: decisions,
	}); err != nil {
		t.Fatalf("SaveTask error = %v", err)
	}
}

func TestAssertion_PolicyDecisionAllowedBy(t *testing.T) {
	const runID = "run-policy-allow"
	run, h := runToTerminal(t, runID, "policy")
	saveDecisionTask(t, h, runID, "decision-task",
		api.PolicyDecision{DecisionID: "egress-allow", Effect: api.PolicyEffectAllow},
	)
	if err := (assertions.PolicyDecisionAllowedBy{Policy: "egress-allow"}).Check(context.Background(), run, h); err != nil {
		t.Fatalf("expected allowed-by to pass, got %v", err)
	}
	if err := (assertions.PolicyDecisionDeniedBy{Policy: "egress-allow"}).Check(context.Background(), run, h); err == nil {
		t.Fatalf("expected denied-by to fail for an allow decision")
	}
}

func TestAssertion_PolicyDecisionDeniedBy_ByMetadata(t *testing.T) {
	const runID = "run-policy-deny"
	run, h := runToTerminal(t, runID, "policy")
	saveDecisionTask(t, h, runID, "decision-task",
		api.PolicyDecision{Effect: api.PolicyEffectDeny, Metadata: map[string]string{"policy": "no-secrets"}},
	)
	if err := (assertions.PolicyDecisionDeniedBy{Policy: "no-secrets"}).Check(context.Background(), run, h); err != nil {
		t.Fatalf("expected denied-by (by metadata) to pass, got %v", err)
	}
}

func TestAssertion_PolicyDecisionAllowedBy_Unknown(t *testing.T) {
	const runID = "run-policy-unknown"
	run, h := runToTerminal(t, runID, "policy")
	if err := (assertions.PolicyDecisionAllowedBy{Policy: "missing"}).Check(context.Background(), run, h); err == nil {
		t.Fatalf("expected unknown policy to fail")
	}
}
