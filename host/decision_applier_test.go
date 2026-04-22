package host

import (
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/internal/blackboard"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

func verifiedBoard() *blackboard.State {
	return &blackboard.State{
		Claims:   []blackboard.Claim{{ID: "c1"}},
		Findings: []blackboard.Finding{{ID: "f1", ClaimIDs: []string{"c1"}}},
		Verifications: []blackboard.VerificationResult{
			{ClaimID: "c1", Status: blackboard.VerificationStatusSupported, Confidence: 0.9, EvidenceIDs: []string{"ev"}},
		},
		Exchanges: []blackboard.Exchange{{ID: "ex-1", Key: "k", ClaimIDs: []string{"c1"}}},
	}
}

func TestApplyDecision_GrantRunAppendsPendingGrant(t *testing.T) {
	digest := SupervisorDigest{
		Sequence: 1,
		Tasks:    []TaskDigest{{ID: "t1", Status: team.TaskStatusPending, Ready: true}},
	}
	control, events, err := ApplyDecision(
		team.ControlState{},
		team.SupervisorDecision{
			Kind: team.DecisionGrantRun, DigestSequence: 1,
			Grants: []team.TaskRunGrant{{TaskID: "t1", Reason: "ready"}},
			Rationale: "unblock t1",
		},
		digest, nil, "run-1", "team-1", time.Now(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(control.PendingGrants) != 1 || control.PendingGrants[0].TaskID != "t1" {
		t.Fatalf("expected pending grant for t1, got %#v", control.PendingGrants)
	}
	if control.DecisionCount != 1 {
		t.Fatalf("expected DecisionCount=1, got %d", control.DecisionCount)
	}
	if control.LastDecision == nil || control.LastDecision.Kind != team.DecisionGrantRun {
		t.Fatalf("expected LastDecision populated, got %#v", control.LastDecision)
	}
	if len(events) != 2 {
		t.Fatalf("expected SupervisorDecision + TaskRunGranted events, got %d: %#v", len(events), events)
	}
	if events[0].Type != storage.EventSupervisorDecision || events[1].Type != storage.EventTaskRunGranted {
		t.Fatalf("expected ordered (decision, granted), got %s then %s", events[0].Type, events[1].Type)
	}
	if events[1].TaskID != "t1" {
		t.Fatalf("expected grant event tagged with taskId, got %q", events[1].TaskID)
	}
}

func TestApplyDecision_GrantRunDeduplicatesByTaskID(t *testing.T) {
	// The supervisor may re-issue a grant with an updated context policy
	// between digests. We merge-in-place rather than stacking grants to
	// avoid having two entries for the same task — the consumer would
	// then run it twice.
	prior := team.ControlState{
		PendingGrants: []team.TaskRunGrant{{TaskID: "t1", Reason: "initial"}},
	}
	digest := SupervisorDigest{
		Sequence: 1,
		Tasks:    []TaskDigest{{ID: "t1", Status: team.TaskStatusPending, Ready: true}},
	}
	control, _, err := ApplyDecision(
		prior,
		team.SupervisorDecision{
			Kind: team.DecisionGrantRun, DigestSequence: 1,
			Grants: []team.TaskRunGrant{{
				TaskID: "t1", Reason: "refreshed",
				ContextPolicy: team.TaskContextPolicy{ForceRefresh: true},
			}},
		},
		digest, nil, "run-1", "team-1", time.Now(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(control.PendingGrants) != 1 {
		t.Fatalf("expected single grant after dedup, got %#v", control.PendingGrants)
	}
	if !control.PendingGrants[0].ContextPolicy.ForceRefresh {
		t.Fatalf("expected grant to be updated with ForceRefresh, got %#v", control.PendingGrants[0])
	}
	if control.PendingGrants[0].Reason != "refreshed" {
		t.Fatalf("expected reason updated, got %q", control.PendingGrants[0].Reason)
	}
}

func TestApplyDecision_SynthesizeFreezesPacket(t *testing.T) {
	digest := SupervisorDigest{Sequence: 1}
	board := verifiedBoard()
	prior := team.ControlState{}
	decision := team.SupervisorDecision{
		Kind: team.DecisionSynthesize, DigestSequence: 1,
		Synthesis: &team.SynthesisRequest{Packet: team.SynthesisPacket{
			Question:   "what is the answer?",
			FindingIDs: []string{"f1"},
			ClaimIDs:   []string{"c1"},
		}},
	}
	control, events, err := ApplyDecision(prior, decision, digest, board, "run-1", "team-1", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if control.Packet == nil {
		t.Fatalf("expected packet committed to ControlState, got nil")
	}
	if control.Packet.Question != "what is the answer?" {
		t.Fatalf("expected question preserved on packet, got %q", control.Packet.Question)
	}
	// Mutating the decision after the fact must not leak into ControlState.
	decision.Synthesis.Packet.FindingIDs[0] = "tampered"
	if control.Packet.FindingIDs[0] != "f1" {
		t.Fatalf("applier must deep-copy the packet, got %#v", control.Packet.FindingIDs)
	}
	var sawPacketEvent bool
	for _, ev := range events {
		if ev.Type == storage.EventSynthesisPacketBuilt {
			sawPacketEvent = true
		}
	}
	if !sawPacketEvent {
		t.Fatalf("expected SynthesisPacketBuilt event, got %#v", events)
	}
}

func TestApplyDecision_RequestVerifyRecordsDecisionOnly(t *testing.T) {
	board := verifiedBoard()
	digest := SupervisorDigest{Sequence: 1}
	control, events, err := ApplyDecision(
		team.ControlState{},
		team.SupervisorDecision{
			Kind: team.DecisionRequestVerify, DigestSequence: 1,
			Verify: &team.VerifyRequest{ClaimIDs: []string{"c1"}},
		},
		digest, board, "run-1", "team-1", time.Now(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(control.PendingGrants) != 0 || control.Packet != nil {
		t.Fatalf("request_verify must not mutate grants/packet, got %#v", control)
	}
	if len(events) != 1 || events[0].Type != storage.EventSupervisorDecision {
		t.Fatalf("expected only the decision event, got %#v", events)
	}
}

func TestApplyDecision_ValidatorFailureLeavesStateUntouched(t *testing.T) {
	prior := team.ControlState{DecisionCount: 7}
	digest := SupervisorDigest{Sequence: 1}
	// DigestSequence mismatch → validator must reject before any mutation.
	control, events, err := ApplyDecision(
		prior,
		team.SupervisorDecision{Kind: team.DecisionGrantRun, DigestSequence: 99},
		digest, nil, "run-1", "team-1", time.Now(),
	)
	if err == nil {
		t.Fatalf("expected validator rejection")
	}
	if control.DecisionCount != 7 {
		t.Fatalf("rejected decision must not advance DecisionCount, got %d", control.DecisionCount)
	}
	if len(events) != 0 {
		t.Fatalf("rejected decision must not emit events, got %#v", events)
	}
}

func TestApplyDecision_ConsumeGrantPopsAndRemoves(t *testing.T) {
	control := &team.ControlState{
		PendingGrants: []team.TaskRunGrant{
			{TaskID: "t1", Reason: "first"},
			{TaskID: "t2", Reason: "second"},
		},
	}
	grant, ok := control.ConsumeGrant("t2")
	if !ok || grant.Reason != "second" {
		t.Fatalf("expected t2 grant, got %#v (ok=%v)", grant, ok)
	}
	if len(control.PendingGrants) != 1 || control.PendingGrants[0].TaskID != "t1" {
		t.Fatalf("expected t1 left after consume, got %#v", control.PendingGrants)
	}
	if _, ok := control.ConsumeGrant("t2"); ok {
		t.Fatalf("grant must be single-use")
	}
}

func TestBuildSynthesisPacket_OnlyPicksVerifiedEvidence(t *testing.T) {
	board := &blackboard.State{
		Claims: []blackboard.Claim{{ID: "c1"}, {ID: "c2"}},
		Findings: []blackboard.Finding{
			{ID: "f1", ClaimIDs: []string{"c1"}},
			{ID: "f2", ClaimIDs: []string{"c2"}},
		},
		Verifications: []blackboard.VerificationResult{
			{ClaimID: "c1", Status: blackboard.VerificationStatusSupported, Confidence: 0.9, EvidenceIDs: []string{"ev"}},
			// c2 intentionally insufficient.
			{ClaimID: "c2", Status: blackboard.VerificationStatusInsufficient, Confidence: 0.9, EvidenceIDs: []string{"ev"}},
		},
		Exchanges: []blackboard.Exchange{
			{ID: "ex-good", Key: "k", ClaimIDs: []string{"c1"}},
			{ID: "ex-bad", Key: "k", ClaimIDs: []string{"c2"}},
		},
	}
	packet := BuildSynthesisPacket(board, "q")
	if packet.Question != "q" {
		t.Fatalf("expected question preserved, got %q", packet.Question)
	}
	if len(packet.FindingIDs) != 1 || packet.FindingIDs[0] != "f1" {
		t.Fatalf("expected only f1 in findings, got %#v", packet.FindingIDs)
	}
	if len(packet.ClaimIDs) != 1 || packet.ClaimIDs[0] != "c1" {
		t.Fatalf("expected only c1 in claims, got %#v", packet.ClaimIDs)
	}
	if len(packet.ExchangeIDs) != 1 || packet.ExchangeIDs[0] != "ex-good" {
		t.Fatalf("expected only ex-good in exchanges, got %#v", packet.ExchangeIDs)
	}
}
