package host

import (
	"testing"

	"github.com/Viking602/go-hydaelyn/internal/blackboard"
	"github.com/Viking602/go-hydaelyn/team"
)

func TestVerificationGateStatus_ContradictedShortCircuitsSupported(t *testing.T) {
	task := team.Task{ID: "verify-1", Kind: team.TaskKindVerify, Result: &team.Result{Summary: "supported"}}
	results := []blackboard.VerificationResult{
		{ClaimID: "claim-1", Status: blackboard.VerificationStatusSupported, Confidence: 0.95},
		{ClaimID: "claim-2", Status: blackboard.VerificationStatusContradicted, Confidence: 0.9},
	}
	if got := verificationGateStatus(task, results); got != blackboard.VerificationStatusContradicted {
		t.Fatalf("expected contradicted gate when any claim is contradicted, got %q", got)
	}
}

func TestVerificationGateStatus_EmptyResultsDegradeToInsufficient(t *testing.T) {
	task := team.Task{ID: "verify-1", Kind: team.TaskKindVerify, Result: &team.Result{Summary: "looks good to me"}}
	if got := verificationGateStatus(task, nil); got != blackboard.VerificationStatusInsufficient {
		t.Fatalf("expected insufficient gate when no results, got %q", got)
	}
}

func TestVerificationGateStatus_AllSupportedPassesOnlyWhenConfidenceClears(t *testing.T) {
	task := team.Task{ID: "verify-1", Kind: team.TaskKindVerify, Result: &team.Result{Summary: ""}}
	// SupportsClaim requires Status=Supported AND confidence >= threshold AND
	// at least one evidence id. Each failure mode must degrade the gate to
	// insufficient — otherwise a weakly-believed or evidence-less claim could
	// slip past synthesis guards.
	underConfident := []blackboard.VerificationResult{
		{ClaimID: "claim-1", Status: blackboard.VerificationStatusSupported, Confidence: 0.1, EvidenceIDs: []string{"ev-1"}},
	}
	if got := verificationGateStatus(task, underConfident); got != blackboard.VerificationStatusInsufficient {
		t.Fatalf("expected insufficient gate for under-confident supported claim, got %q", got)
	}
	noEvidence := []blackboard.VerificationResult{
		{ClaimID: "claim-1", Status: blackboard.VerificationStatusSupported, Confidence: 0.95},
	}
	if got := verificationGateStatus(task, noEvidence); got != blackboard.VerificationStatusInsufficient {
		t.Fatalf("expected insufficient gate for evidence-less supported claim, got %q", got)
	}
	fullySupported := []blackboard.VerificationResult{
		{ClaimID: "claim-1", Status: blackboard.VerificationStatusSupported, Confidence: 0.95, EvidenceIDs: []string{"ev-1"}},
	}
	if got := verificationGateStatus(task, fullySupported); got != blackboard.VerificationStatusSupported {
		t.Fatalf("expected supported gate when every claim is supported at threshold with evidence, got %q", got)
	}
}

func TestStructuredVerificationStatus_EmptyDecisionReturnsInsufficient(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
	}{
		{name: "missing decision field", payload: map[string]any{"claim_id": "claim-1"}},
		{name: "empty decision string", payload: map[string]any{"decision": ""}},
		{name: "whitespace-only decision", payload: map[string]any{"decision": "   "}},
	}
	// A summary strongly suggesting "supported" must not smuggle approval past
	// the structured gate — verifiers without an explicit decision contribute
	// no evidence.
	const summary = "everything looks supported and well"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := structuredVerificationStatus(tc.payload, summary); got != blackboard.VerificationStatusInsufficient {
				t.Fatalf("expected insufficient for %s, got %q", tc.name, got)
			}
		})
	}
}

func TestStructuredVerificationStatus_UnrecognizedDecisionReturnsInsufficient(t *testing.T) {
	if got := structuredVerificationStatus(map[string]any{"decision": "probably-ok"}, "supported"); got != blackboard.VerificationStatusInsufficient {
		t.Fatalf("expected insufficient for unrecognized decision, got %q", got)
	}
}

func TestStructuredVerificationStatus_KnownDecisionsStillMap(t *testing.T) {
	cases := map[string]blackboard.VerificationStatus{
		"supported":    blackboard.VerificationStatusSupported,
		"pass":         blackboard.VerificationStatusSupported,
		"approved":     blackboard.VerificationStatusSupported,
		"contradicted": blackboard.VerificationStatusContradicted,
		"unsupported":  blackboard.VerificationStatusContradicted,
		"blocked":      blackboard.VerificationStatusContradicted,
		"false":        blackboard.VerificationStatusContradicted,
	}
	for decision, want := range cases {
		if got := structuredVerificationStatus(map[string]any{"decision": decision}, ""); got != want {
			t.Fatalf("decision %q: expected %q, got %q", decision, want, got)
		}
	}
}
