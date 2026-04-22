package host

import (
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/internal/blackboard"
	"github.com/Viking602/go-hydaelyn/team"
)

func TestBuildSupervisorDigest_PureReplay(t *testing.T) {
	// Same inputs must produce byte-identical digests. The supervisor is
	// allowed to call the observer multiple times per tick; if the digest
	// changed on re-run, downstream decision signing / event ordering
	// would be non-deterministic.
	state := team.RunState{
		ID:     "run-1",
		Status: team.StatusRunning,
		Phase:  team.PhaseResearch,
		Tasks: []team.Task{
			{ID: "t1", Kind: team.TaskKindResearch, Status: team.TaskStatusCompleted, Result: &team.Result{Confidence: 0.8}},
			{ID: "t2", Kind: team.TaskKindVerify, Status: team.TaskStatusPending, DependsOn: []string{"t1"}},
		},
	}
	cursor := team.SupervisorCursor{}
	now := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)

	a, curA := BuildSupervisorDigest(state, cursor, now)
	b, curB := BuildSupervisorDigest(state, cursor, now)
	if a.Sequence != b.Sequence || len(a.Tasks) != len(b.Tasks) {
		t.Fatalf("non-deterministic digest between calls: %#v vs %#v", a, b)
	}
	if curA.TaskVersions["t1"] != curB.TaskVersions["t1"] {
		t.Fatalf("cursor advanced non-deterministically: %#v vs %#v", curA, curB)
	}
}

func TestBuildSupervisorDigest_PendingReadsSurfaceFromBlockers(t *testing.T) {
	state := team.RunState{
		Blackboard: &blackboard.State{},
		Tasks: []team.Task{
			{ID: "need-input", Status: team.TaskStatusPending, Reads: []string{"research.notes"}},
		},
	}
	digest, _ := BuildSupervisorDigest(state, team.SupervisorCursor{}, time.Now())
	if len(digest.PendingReads) != 1 {
		t.Fatalf("expected one pending read bubbled up, got %#v", digest.PendingReads)
	}
	if digest.PendingReads[0].Target != "research.notes" {
		t.Fatalf("expected target research.notes, got %q", digest.PendingReads[0].Target)
	}
	// Also check the task digest reflects the same blocker so supervisors
	// scanning per-task readiness don't disagree with the top-level list.
	if len(digest.Tasks) != 1 || digest.Tasks[0].Ready {
		t.Fatalf("expected the task to be not-ready, got %#v", digest.Tasks)
	}
}

func TestBuildSupervisorDigest_CursorBoundsRecentFindings(t *testing.T) {
	// The cursor must prevent already-observed findings from reappearing
	// — otherwise every tick would re-notify the supervisor of the same
	// facts and burn context.
	board := &blackboard.State{
		Findings: []blackboard.Finding{
			{ID: "f1", ClaimIDs: []string{"c1"}},
			{ID: "f2", ClaimIDs: []string{"c2"}},
		},
		Claims: []blackboard.Claim{{ID: "c1"}, {ID: "c2"}},
		Verifications: []blackboard.VerificationResult{
			{ClaimID: "c1", Status: blackboard.VerificationStatusSupported, Confidence: 0.9, EvidenceIDs: []string{"ev"}},
			{ClaimID: "c2", Status: blackboard.VerificationStatusSupported, Confidence: 0.9, EvidenceIDs: []string{"ev"}},
		},
	}
	state := team.RunState{Blackboard: board}

	first, cursor := BuildSupervisorDigest(state, team.SupervisorCursor{}, time.Now())
	if len(first.RecentFindings) != 2 {
		t.Fatalf("first pass should see both findings, got %#v", first.RecentFindings)
	}
	for _, f := range first.RecentFindings {
		if !f.Supported {
			t.Fatalf("expected finding %s supported once its claim passes verification, got %#v", f.ID, f)
		}
	}

	second, _ := BuildSupervisorDigest(state, cursor, time.Now())
	if len(second.RecentFindings) != 0 {
		t.Fatalf("second pass with advanced cursor should see no recent findings, got %#v", second.RecentFindings)
	}
}

func TestBuildSupervisorDigest_FindingSupportedOnlyWhenEveryClaimVerifies(t *testing.T) {
	board := &blackboard.State{
		Findings: []blackboard.Finding{
			{ID: "f1", ClaimIDs: []string{"c1", "c2"}},
		},
		Claims: []blackboard.Claim{{ID: "c1"}, {ID: "c2"}},
		Verifications: []blackboard.VerificationResult{
			{ClaimID: "c1", Status: blackboard.VerificationStatusSupported, Confidence: 0.9, EvidenceIDs: []string{"ev"}},
			// c2 intentionally contradicted.
			{ClaimID: "c2", Status: blackboard.VerificationStatusContradicted, Confidence: 0.9, EvidenceIDs: []string{"ev"}},
		},
	}
	digest, _ := BuildSupervisorDigest(team.RunState{Blackboard: board}, team.SupervisorCursor{}, time.Now())
	if len(digest.RecentFindings) != 1 {
		t.Fatalf("expected one finding surfaced, got %#v", digest.RecentFindings)
	}
	if digest.RecentFindings[0].Supported {
		t.Fatalf("expected finding with a contradicted claim to be unsupported, got %#v", digest.RecentFindings[0])
	}
}

func TestBuildSupervisorDigest_ConflictsAttached(t *testing.T) {
	board := &blackboard.State{
		Exchanges: []blackboard.Exchange{
			{ID: "ex-1", Key: "design.doc", TaskID: "a", Text: "alpha"},
			{ID: "ex-2", Key: "design.doc", TaskID: "b", Text: "beta"},
		},
	}
	digest, _ := BuildSupervisorDigest(team.RunState{Blackboard: board}, team.SupervisorCursor{}, time.Now())
	if len(digest.Conflicts) != 1 || digest.Conflicts[0].Key != "design.doc" {
		t.Fatalf("expected conflict on design.doc, got %#v", digest.Conflicts)
	}
}

func TestBuildSupervisorDigest_BudgetRollup(t *testing.T) {
	state := team.RunState{
		Tasks: []team.Task{
			{ID: "t1", Status: team.TaskStatusCompleted, Result: &team.Result{ToolCallCount: 3}},
			{ID: "t2", Status: team.TaskStatusFailed},
			{ID: "t3", Status: team.TaskStatusPending},
			{ID: "t4", Status: team.TaskStatusRunning},
		},
	}
	digest, _ := BuildSupervisorDigest(state, team.SupervisorCursor{}, time.Now())
	b := digest.Budget
	if b.TasksCompleted != 1 || b.TasksFailed != 1 || b.TasksPending != 1 || b.TasksRunning != 1 {
		t.Fatalf("budget rollup incorrect: %#v", b)
	}
	if b.ToolCallsUsed != 3 {
		t.Fatalf("expected tool calls from completed task's Result, got %d", b.ToolCallsUsed)
	}
}

func TestBuildSupervisorDigest_SequenceIncrementsWithDigestCount(t *testing.T) {
	state := team.RunState{
		Control: &team.ControlState{DigestCount: 4},
	}
	digest, _ := BuildSupervisorDigest(state, team.SupervisorCursor{}, time.Now())
	if digest.Sequence != 5 {
		t.Fatalf("expected sequence 5 given DigestCount=4, got %d", digest.Sequence)
	}
}
