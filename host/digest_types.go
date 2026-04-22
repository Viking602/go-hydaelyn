package host

import (
	"time"

	"github.com/Viking602/go-hydaelyn/internal/blackboard"
	"github.com/Viking602/go-hydaelyn/team"
)

// SupervisorDigest is the snapshot the supervisor sees at one observation
// tick. It is deliberately a *derived view* — everything here comes from
// RunState + Blackboard + ControlState, so the digest is replayable: given
// the same inputs we produce the same digest, byte for byte.
//
// The supervisor never mutates the digest. Decisions derive from it; PR 4
// will introduce the Decision schema that consumes this type.
type SupervisorDigest struct {
	RunID         string                `json:"runId,omitempty"`
	TeamID        string                `json:"teamId,omitempty"`
	Sequence      int                   `json:"sequence"`
	ObservedAt    time.Time             `json:"observedAt"`
	Phase         team.Phase            `json:"phase,omitempty"`
	Status        team.Status           `json:"status,omitempty"`
	Tasks         []TaskDigest          `json:"tasks,omitempty"`
	PendingReads  []PendingReadDigest   `json:"pendingReads,omitempty"`
	RecentFindings []FindingDigest      `json:"recentFindings,omitempty"`
	RecentClaims   []ClaimDigest        `json:"recentClaims,omitempty"`
	Conflicts     []blackboard.Conflict `json:"conflicts,omitempty"`
	Budget        BudgetDigest          `json:"budget,omitempty"`
	Cursor        team.SupervisorCursor `json:"cursor"`
}

// TaskDigest distills a Task to only what the supervisor needs. We
// intentionally omit large fields (Result payloads, tool output) — the
// supervisor should reason about *state*, not replay worker transcripts.
type TaskDigest struct {
	ID           string          `json:"id"`
	Kind         team.TaskKind   `json:"kind,omitempty"`
	Stage        team.TaskStage  `json:"stage,omitempty"`
	Status       team.TaskStatus `json:"status"`
	Assignee     string          `json:"assignee,omitempty"`
	DependsOn    []string        `json:"dependsOn,omitempty"`
	Attempts     int             `json:"attempts,omitempty"`
	Confidence   float64         `json:"confidence,omitempty"`
	Error        string          `json:"error,omitempty"`
	Blockers     []team.Blocker  `json:"blockers,omitempty"`
	Ready        bool            `json:"ready,omitempty"`
}

// PendingReadDigest mirrors team.Blocker for reads, but pulled to the top
// level so the supervisor can scan "which required inputs are missing"
// without walking every task.
type PendingReadDigest struct {
	TaskID string `json:"taskId"`
	Target string `json:"target"`
	Reason string `json:"reason,omitempty"`
}

// FindingDigest narrows a blackboard.Finding to supervisor-relevant fields.
// Supported is computed using the same SupportsClaim gate the data plane
// uses, so the supervisor's view of "this is verified" matches what
// downstream selectors will accept.
type FindingDigest struct {
	ID         string   `json:"id"`
	TaskID     string   `json:"taskId,omitempty"`
	Summary    string   `json:"summary,omitempty"`
	ClaimIDs   []string `json:"claimIds,omitempty"`
	Supported  bool     `json:"supported"`
	Confidence float64  `json:"confidence,omitempty"`
}

// ClaimDigest surfaces verification state per claim. A claim's supported
// bool reflects the latest matching VerificationResult; unverified claims
// still appear so the supervisor can request verify runs.
type ClaimDigest struct {
	ID                   string                        `json:"id"`
	TaskID               string                        `json:"taskId,omitempty"`
	Summary              string                        `json:"summary,omitempty"`
	Supported            bool                          `json:"supported"`
	VerificationStatus   blackboard.VerificationStatus `json:"verificationStatus,omitempty"`
	Confidence           float64                       `json:"confidence,omitempty"`
}

// BudgetDigest is a coarse rollup of run-level consumption. Detailed
// per-task usage lives on the Task/Result records — here we keep only the
// aggregates the supervisor needs to enforce budget caps.
type BudgetDigest struct {
	TasksCompleted int `json:"tasksCompleted,omitempty"`
	TasksFailed    int `json:"tasksFailed,omitempty"`
	TasksPending   int `json:"tasksPending,omitempty"`
	TasksRunning   int `json:"tasksRunning,omitempty"`
	TokensUsed     int `json:"tokensUsed,omitempty"`
	ToolCallsUsed  int `json:"toolCallsUsed,omitempty"`
}
