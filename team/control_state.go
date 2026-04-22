package team

import "time"

// ControlState is the supervisor's control-plane bookkeeping: what it last
// observed, where its cursor is parked, and how many digests it has emitted.
// It lives beside RunState rather than inside it so the data plane (tasks,
// blackboard) and the control plane can evolve independently — the
// supervisor is allowed to re-read the same state; workers are not allowed
// to read control-plane progress.
type ControlState struct {
	Cursor       SupervisorCursor `json:"cursor,omitempty"`
	LastObserved time.Time        `json:"lastObserved,omitempty"`
	DigestCount  int              `json:"digestCount,omitempty"`
	DecisionCount int             `json:"decisionCount,omitempty"`
}

// SupervisorCursor captures the observation boundary: how much of the data
// plane the supervisor has already folded into a digest. The next
// observation produces only the delta past this cursor so large runs don't
// replay the whole blackboard each tick.
//
// Exchange/Verification/Finding indices are counts (not ids) because the
// blackboard slices are append-only — an index of N means "the first N
// entries have been observed". TaskVersions tracks per-task version numbers
// so we can detect re-runs even when task status is unchanged.
type SupervisorCursor struct {
	ExchangeIndex     int            `json:"exchangeIndex,omitempty"`
	VerificationIndex int            `json:"verificationIndex,omitempty"`
	FindingIndex      int            `json:"findingIndex,omitempty"`
	TaskVersions      map[string]int `json:"taskVersions,omitempty"`
	EventSequence     int            `json:"eventSequence,omitempty"`
}

// Clone returns a deep copy so advancing the cursor on a derived value
// doesn't mutate the caller's state.
func (c SupervisorCursor) Clone() SupervisorCursor {
	out := c
	if len(c.TaskVersions) > 0 {
		out.TaskVersions = make(map[string]int, len(c.TaskVersions))
		for k, v := range c.TaskVersions {
			out.TaskVersions[k] = v
		}
	}
	return out
}
