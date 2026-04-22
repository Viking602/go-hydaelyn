package team

import "time"

// DecisionKind enumerates the narrow set of verbs v1 of the supervisor is
// allowed to emit. The set is intentionally small: every extra verb
// expands the validator's responsibilities and the audit surface.
type DecisionKind string

const (
	// DecisionGrantRun authorizes one or more pending tasks to execute.
	// Without a grant, strict-mode execution refuses to dispatch — this
	// is what makes the supervisor authoritative rather than advisory.
	DecisionGrantRun DecisionKind = "grant_run"

	// DecisionRequestVerify instructs the verifier to adjudicate specific
	// claims. The supervisor uses this when findings accumulate without
	// verifications attached, or when a contradicted verification needs
	// re-adjudication with new evidence.
	DecisionRequestVerify DecisionKind = "request_verify"

	// DecisionSynthesize commits a bundle of verified evidence as the
	// final-answer input. Once applied, the synthesis packet is frozen
	// and workers can no longer mutate its referenced exchanges.
	DecisionSynthesize DecisionKind = "synthesize"
)

// SupervisorDecision is the atomic unit the supervisor produces per tick.
// Exactly one of Grants / Verify / Synthesis is populated; which one is
// determined by Kind. DigestSequence anchors the decision to the digest
// it was made against so stale decisions (observed state has moved on)
// can be rejected by the validator.
type SupervisorDecision struct {
	Kind            DecisionKind      `json:"kind"`
	DigestSequence  int               `json:"digestSequence"`
	Grants          []TaskRunGrant    `json:"grants,omitempty"`
	Verify          *VerifyRequest    `json:"verify,omitempty"`
	Synthesis       *SynthesisRequest `json:"synthesis,omitempty"`
	Rationale       string            `json:"rationale,omitempty"`
}

// TaskRunGrant authorizes one pending task to execute under the attached
// context policy. Grants are single-use: once the executor consumes a
// grant, it is removed from ControlState.PendingGrants.
type TaskRunGrant struct {
	TaskID        string            `json:"taskId"`
	Reason        string            `json:"reason,omitempty"`
	ContextPolicy TaskContextPolicy `json:"contextPolicy,omitempty"`
}

// TaskContextPolicy tells the executor how fresh the task's blackboard
// reads must be. MinExchangeIndex prevents a task from running against a
// stale materialization — the stale-read fix PR 5 wires up.
//
// ForceRefresh short-circuits any input cache and re-materializes
// selectors at dispatch time. It is useful when the supervisor wants to
// dispatch a retry against newly-published evidence without inventing a
// new task id.
type TaskContextPolicy struct {
	ForceRefresh     bool `json:"forceRefresh,omitempty"`
	MinExchangeIndex int  `json:"minExchangeIndex,omitempty"`
}

// VerifyRequest enumerates the claims the verifier must adjudicate. The
// validator enforces that every ClaimID exists on the board so the
// supervisor can't invent claims out of thin air.
type VerifyRequest struct {
	ClaimIDs []string `json:"claimIds"`
	Reason   string   `json:"reason,omitempty"`
}

// SynthesisRequest commits a verified-evidence bundle to a final answer.
// The Question field is optional — it lets the supervisor reshape the
// user's ask on the way to synthesis — but the evidence references
// (Packet) are mandatory and must all structurally point at Supported
// claims / verified exchanges.
type SynthesisRequest struct {
	Packet SynthesisPacket `json:"packet"`
	Reason string          `json:"reason,omitempty"`
}

// SynthesisPacket is the immutable evidence bundle the synthesizer
// consumes. The invariant enforced by the validator: every id here must
// resolve to something the Supported/Verified gate already accepts —
// that way the final answer's evidence cannot structurally exceed what
// the verifier certified.
type SynthesisPacket struct {
	Question    string   `json:"question,omitempty"`
	FindingIDs  []string `json:"findingIds,omitempty"`
	ClaimIDs    []string `json:"claimIds,omitempty"`
	ExchangeIDs []string `json:"exchangeIds,omitempty"`
}

// DecisionRecord is the minimal audit trail we persist on ControlState so
// replay can tie an applied decision back to the digest it was made
// against. The full decision payload lives on the
// EventSupervisorDecision event; this record exists so in-memory
// state transitions remain coherent without reading the event log.
type DecisionRecord struct {
	Kind           DecisionKind `json:"kind"`
	DigestSequence int          `json:"digestSequence"`
	AppliedAt      time.Time    `json:"appliedAt"`
	Rationale      string       `json:"rationale,omitempty"`
}
