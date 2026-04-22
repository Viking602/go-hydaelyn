package host

import (
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/internal/blackboard"
	"github.com/Viking602/go-hydaelyn/team"
)

func TestValidateDecision_StaleDigestRejected(t *testing.T) {
	decision := team.SupervisorDecision{
		Kind:           team.DecisionGrantRun,
		DigestSequence: 3,
		Grants:         []team.TaskRunGrant{{TaskID: "t1"}},
	}
	digest := SupervisorDigest{Sequence: 5}
	err := ValidateDecision(decision, digest, nil)
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected stale-decision rejection, got %v", err)
	}
}

func TestValidateDecision_UnknownKindRejected(t *testing.T) {
	decision := team.SupervisorDecision{Kind: "mutate", DigestSequence: 1}
	digest := SupervisorDigest{Sequence: 1}
	err := ValidateDecision(decision, digest, nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported-kind rejection, got %v", err)
	}
}

func TestValidateDecision_GrantRunRequiresReadyPendingTask(t *testing.T) {
	digest := SupervisorDigest{
		Sequence: 1,
		Tasks: []TaskDigest{
			{ID: "t-ready", Status: team.TaskStatusPending, Ready: true},
			{ID: "t-blocked", Status: team.TaskStatusPending, Ready: false, Blockers: []team.Blocker{{Kind: team.BlockerReadMissing, Target: "x"}}},
			{ID: "t-running", Status: team.TaskStatusRunning, Ready: false},
		},
	}

	// Ready task → OK.
	if err := ValidateDecision(team.SupervisorDecision{
		Kind: team.DecisionGrantRun, DigestSequence: 1,
		Grants: []team.TaskRunGrant{{TaskID: "t-ready"}},
	}, digest, nil); err != nil {
		t.Fatalf("expected ready task grant to pass, got %v", err)
	}

	// Blocked task → must fail.
	err := ValidateDecision(team.SupervisorDecision{
		Kind: team.DecisionGrantRun, DigestSequence: 1,
		Grants: []team.TaskRunGrant{{TaskID: "t-blocked"}},
	}, digest, nil)
	if err == nil || !strings.Contains(err.Error(), "blockers") {
		t.Fatalf("expected blocked task rejection, got %v", err)
	}

	// Running task → must fail (not pending).
	err = ValidateDecision(team.SupervisorDecision{
		Kind: team.DecisionGrantRun, DigestSequence: 1,
		Grants: []team.TaskRunGrant{{TaskID: "t-running"}},
	}, digest, nil)
	if err == nil || !strings.Contains(err.Error(), "pending") {
		t.Fatalf("expected non-pending rejection, got %v", err)
	}

	// Unknown task → must fail.
	err = ValidateDecision(team.SupervisorDecision{
		Kind: team.DecisionGrantRun, DigestSequence: 1,
		Grants: []team.TaskRunGrant{{TaskID: "phantom"}},
	}, digest, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown task") {
		t.Fatalf("expected unknown-task rejection, got %v", err)
	}
}

func TestValidateDecision_GrantRunRefusesDuplicateTask(t *testing.T) {
	digest := SupervisorDigest{
		Sequence: 1,
		Tasks:    []TaskDigest{{ID: "t1", Status: team.TaskStatusPending, Ready: true}},
	}
	err := ValidateDecision(team.SupervisorDecision{
		Kind: team.DecisionGrantRun, DigestSequence: 1,
		Grants: []team.TaskRunGrant{{TaskID: "t1"}, {TaskID: "t1"}},
	}, digest, nil)
	if err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("expected duplicate-grant rejection, got %v", err)
	}
}

func TestValidateDecision_GrantRunMustNotCarryVerifyOrSynthesis(t *testing.T) {
	digest := SupervisorDigest{
		Sequence: 1,
		Tasks:    []TaskDigest{{ID: "t1", Status: team.TaskStatusPending, Ready: true}},
	}
	decision := team.SupervisorDecision{
		Kind:           team.DecisionGrantRun,
		DigestSequence: 1,
		Grants:         []team.TaskRunGrant{{TaskID: "t1"}},
		Verify:         &team.VerifyRequest{ClaimIDs: []string{"c1"}},
	}
	err := ValidateDecision(decision, digest, nil)
	if err == nil {
		t.Fatalf("grant_run cross-carrying verify payload must be rejected, got nil")
	}
}

func TestValidateDecision_RequestVerifyNeedsExistingClaims(t *testing.T) {
	digest := SupervisorDigest{Sequence: 1}
	board := &blackboard.State{Claims: []blackboard.Claim{{ID: "c-real"}}}

	if err := ValidateDecision(team.SupervisorDecision{
		Kind: team.DecisionRequestVerify, DigestSequence: 1,
		Verify: &team.VerifyRequest{ClaimIDs: []string{"c-real"}},
	}, digest, board); err != nil {
		t.Fatalf("expected existing-claim verify to pass, got %v", err)
	}

	err := ValidateDecision(team.SupervisorDecision{
		Kind: team.DecisionRequestVerify, DigestSequence: 1,
		Verify: &team.VerifyRequest{ClaimIDs: []string{"c-phantom"}},
	}, digest, board)
	if err == nil || !strings.Contains(err.Error(), "unknown claim") {
		t.Fatalf("expected unknown-claim rejection, got %v", err)
	}
}

func TestValidateDecision_SynthesizeEnforcesSupportedEvidence(t *testing.T) {
	digest := SupervisorDigest{Sequence: 1}
	board := &blackboard.State{
		Claims: []blackboard.Claim{{ID: "c1"}, {ID: "c2"}},
		Findings: []blackboard.Finding{
			{ID: "f-good", ClaimIDs: []string{"c1"}},
			{ID: "f-bad", ClaimIDs: []string{"c2"}},
		},
		Verifications: []blackboard.VerificationResult{
			{ClaimID: "c1", Status: blackboard.VerificationStatusSupported, Confidence: 0.9, EvidenceIDs: []string{"ev"}},
			{ClaimID: "c2", Status: blackboard.VerificationStatusContradicted, Confidence: 0.9, EvidenceIDs: []string{"ev"}},
		},
		Exchanges: []blackboard.Exchange{
			{ID: "ex-good", Key: "k", ClaimIDs: []string{"c1"}},
			{ID: "ex-bad", Key: "k", ClaimIDs: []string{"c2"}},
		},
	}

	// Supported finding + verified claim + verified exchange: pass.
	ok := team.SupervisorDecision{
		Kind: team.DecisionSynthesize, DigestSequence: 1,
		Synthesis: &team.SynthesisRequest{Packet: team.SynthesisPacket{
			FindingIDs:  []string{"f-good"},
			ClaimIDs:    []string{"c1"},
			ExchangeIDs: []string{"ex-good"},
		}},
	}
	if err := ValidateDecision(ok, digest, board); err != nil {
		t.Fatalf("expected verified packet to pass, got %v", err)
	}

	// Unsupported finding: must fail.
	err := ValidateDecision(team.SupervisorDecision{
		Kind: team.DecisionSynthesize, DigestSequence: 1,
		Synthesis: &team.SynthesisRequest{Packet: team.SynthesisPacket{FindingIDs: []string{"f-bad"}}},
	}, digest, board)
	if err == nil || !strings.Contains(err.Error(), "unsupported finding") {
		t.Fatalf("expected unsupported-finding rejection, got %v", err)
	}

	// Contradicted claim: must fail.
	err = ValidateDecision(team.SupervisorDecision{
		Kind: team.DecisionSynthesize, DigestSequence: 1,
		Synthesis: &team.SynthesisRequest{Packet: team.SynthesisPacket{ClaimIDs: []string{"c2"}}},
	}, digest, board)
	if err == nil || !strings.Contains(err.Error(), "unsupported claim") {
		t.Fatalf("expected contradicted-claim rejection, got %v", err)
	}

	// Exchange whose claim is contradicted: must fail.
	err = ValidateDecision(team.SupervisorDecision{
		Kind: team.DecisionSynthesize, DigestSequence: 1,
		Synthesis: &team.SynthesisRequest{Packet: team.SynthesisPacket{ExchangeIDs: []string{"ex-bad"}}},
	}, digest, board)
	if err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("expected unverified-exchange rejection, got %v", err)
	}

	// Empty packet: must fail.
	err = ValidateDecision(team.SupervisorDecision{
		Kind: team.DecisionSynthesize, DigestSequence: 1,
		Synthesis: &team.SynthesisRequest{Packet: team.SynthesisPacket{}},
	}, digest, board)
	if err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("expected empty-packet rejection, got %v", err)
	}
}
