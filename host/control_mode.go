package host

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Viking602/go-hydaelyn/internal/blackboard"
	"github.com/Viking602/go-hydaelyn/team"
)

// ControlMode selects how the supervisor's control plane interacts with the
// execution plane.
//
// Legacy (the default) preserves pre-supervisor behavior: the engine drives
// tasks purely off RunnableTasks and never builds digests. Every existing
// test and recipe keeps passing untouched.
//
// Hybrid emits SupervisorObserved/ConflictRaised and invokes a decider if
// one is registered, but does NOT gate execution on grants — the decider is
// advisory. This is the "turn on observation without changing dispatch"
// mode and is safe to flip on for every run.
//
// Strict enforces grants: a pending task only runs after the control plane
// has issued a TaskRunGrant for it. If a decider is registered it is the
// authoritative grantor; if none is registered we fall back to
// auto-granting ready tasks so Strict + no-decider is equivalent to Hybrid
// from the executor's perspective but still exercises the full event chain
// (observe → decide → grant → dispatch → consume). This fallback exists so
// flipping on Strict doesn't require a decider implementation first — it's
// the minimal thing that lets replay invariants in PR 7 work end-to-end.
type ControlMode string

const (
	ControlModeLegacy ControlMode = ""
	ControlModeHybrid ControlMode = "hybrid"
	ControlModeStrict ControlMode = "strict"
)

// SupervisorDecider converts an observation into zero or more decisions.
// Implementations are called once per control-loop tick with the fresh
// digest; they may return multiple decisions (e.g. grant two ready tasks +
// request verification on a third claim) which are applied in order.
//
// Returning an empty slice is legal and means "no action this tick" — the
// executor will simply not dispatch anything new in strict mode.
type SupervisorDecider interface {
	Decide(ctx context.Context, digest SupervisorDigest, board *blackboard.State) ([]team.SupervisorDecision, error)
}

// SupervisorDeciderFunc adapts a plain function into a SupervisorDecider so
// callers don't need to declare a type just to wire in a closure.
type SupervisorDeciderFunc func(ctx context.Context, digest SupervisorDigest, board *blackboard.State) ([]team.SupervisorDecision, error)

func (f SupervisorDeciderFunc) Decide(ctx context.Context, digest SupervisorDigest, board *blackboard.State) ([]team.SupervisorDecision, error) {
	return f(ctx, digest, board)
}

// observeAndDecide runs the control-plane tick: build the digest, emit the
// observation event, call the decider (or auto-grant), validate + apply
// every returned decision to ControlState, and persist the associated
// events. The returned state is the post-tick RunState with a refreshed
// ControlState; callers should thread it through persistTeamProgress so
// subsequent readiness checks see the new grants.
//
// In Legacy mode this is a no-op that returns the input state unchanged.
// Hybrid emits events but does NOT apply decisions that would gate
// execution — specifically, PendingGrants are cleared before returning so
// executeTasks stays on its legacy path. Strict applies everything.
func (r *Runtime) observeAndDecide(ctx context.Context, state team.RunState) (team.RunState, error) {
	mode := r.effectiveControlMode()
	if mode == ControlModeLegacy {
		return state, nil
	}
	control := state.Control
	if control == nil {
		control = &team.ControlState{}
	}
	cursor := control.Cursor
	digest, nextCursor := BuildSupervisorDigest(state, cursor, time.Now().UTC())

	control.Cursor = nextCursor
	control.DigestCount = digest.Sequence
	control.LastObserved = digest.ObservedAt
	state.Control = control

	if err := r.emitSupervisorObserved(ctx, state.ID, state.ID, digest); err != nil {
		return state, err
	}
	for _, conflict := range digest.Conflicts {
		if err := r.emitConflictRaised(ctx, state.ID, state.ID, conflict); err != nil {
			return state, err
		}
	}

	decisions, err := r.produceDecisions(ctx, mode, digest, state.Blackboard)
	if err != nil {
		return state, err
	}

	now := time.Now().UTC()
	for _, decision := range decisions {
		nextControl, events, applyErr := ApplyDecision(*control, decision, digest, state.Blackboard, state.ID, state.ID, now)
		if applyErr != nil {
			// An invalid decision is rejected outright: record nothing,
			// preserve prior state. Callers see no error so the control
			// loop keeps spinning — a bad decider should not fail a run.
			continue
		}
		*control = nextControl
		for _, ev := range events {
			if err := r.appendEvent(ctx, ev); err != nil {
				return state, err
			}
		}
	}

	// Hybrid runs the full observe+decide+emit chain but must not gate
	// execution: wipe any grants we just recorded so executeTasks stays on
	// the legacy-runnable path. The events have already been written so
	// the audit trail still shows what the decider wanted.
	if mode == ControlModeHybrid {
		control.PendingGrants = nil
	}

	state.Control = control
	return state, nil
}

// produceDecisions is the "who grants what" fork. If the operator wired a
// decider, we defer to it. Otherwise, in Strict mode we auto-grant every
// task the readiness layer already says is Ready; in Hybrid mode we return
// no decisions (the control chain is advisory-only there).
func (r *Runtime) produceDecisions(ctx context.Context, mode ControlMode, digest SupervisorDigest, board *blackboard.State) ([]team.SupervisorDecision, error) {
	if r.supervisorDecider != nil {
		decisions, err := r.supervisorDecider.Decide(ctx, digest, board)
		if err != nil {
			return nil, err
		}
		return decisions, nil
	}
	if mode != ControlModeStrict {
		return nil, nil
	}
	return autoGrantReadyTasks(digest), nil
}

// autoGrantReadyTasks is the "Strict without a decider" fallback. It grants
// exactly the tasks the readiness layer would have dispatched anyway — so
// behavior is observationally identical to legacy dispatch, but routed
// through the full control-plane event chain. This is what lets operators
// flip ControlModeStrict on for CI visibility without having to implement a
// decider first.
func autoGrantReadyTasks(digest SupervisorDigest) []team.SupervisorDecision {
	grants := make([]team.TaskRunGrant, 0)
	for _, task := range digest.Tasks {
		if task.Status != team.TaskStatusPending || !task.Ready {
			continue
		}
		grants = append(grants, team.TaskRunGrant{
			TaskID: task.ID,
			Reason: "auto-granted (no decider registered)",
		})
	}
	if len(grants) == 0 {
		return nil
	}
	return []team.SupervisorDecision{{
		Kind:           team.DecisionGrantRun,
		DigestSequence: digest.Sequence,
		Grants:         grants,
		Rationale:      "auto-grant fallback",
	}}
}

// filterRunnableByGrants is the strict-mode dispatch gate. Given the
// runnable set the engine would normally execute, we keep only tasks that
// hold a pending grant. Grants are consumed here (the state mutation
// happens in-place on state.Control) — a grant runs its task exactly once.
func (r *Runtime) filterRunnableByGrants(state *team.RunState, runnable []team.Task, runnableSet map[string]struct{}) ([]team.Task, map[string]struct{}) {
	if r.effectiveControlMode() != ControlModeStrict {
		return runnable, runnableSet
	}
	if state.Control == nil {
		// Strict mode with no control state can't dispatch anything — every
		// task awaits a grant that will never come. Returning empty here
		// makes that failure mode loud (no progress) rather than silent.
		return nil, map[string]struct{}{}
	}
	filtered := runnable[:0:len(runnable)]
	filteredSet := make(map[string]struct{}, len(runnable))
	for _, task := range runnable {
		if _, ok := runnableSet[task.ID]; !ok {
			continue
		}
		if !state.Control.HasPendingGrant(task.ID) {
			continue
		}
		filtered = append(filtered, task)
		filteredSet[task.ID] = struct{}{}
	}
	// Consume the grants we're about to dispatch. Doing it here — before
	// executeTasks kicks off any goroutines — keeps the invariant simple:
	// a grant visible to readiness is a grant that will produce exactly
	// one dispatch attempt.
	for _, task := range filtered {
		_, _ = state.Control.ConsumeGrant(task.ID)
	}
	return filtered, filteredSet
}

// ensureFreshTaskContext enforces the stale-read invariant attached to
// TaskRunGrant.ContextPolicy. If the grant carries MinExchangeIndex > N
// and the current board has only N exchanges for the selector's keys, the
// dispatch is deferred — we surface a retriable error rather than run the
// task against an older data-plane snapshot. ForceRefresh bypasses any
// message cache; since we re-load from session each dispatch anyway, the
// flag is recorded on the consumed grant for observability but does not
// alter materialization directly.
//
// Returning (nil, nil) means the context is fresh enough and dispatch can
// proceed. Returning (err, nil) is a hard failure. Returning (nil, <non-zero
// duration>) indicates the caller should defer dispatch by that long — PR 7
// can upgrade this to a backoff. For v1 we keep it simple: fresh or abort.
func ensureFreshTaskContext(board *blackboard.State, grant team.TaskRunGrant) error {
	if grant.ContextPolicy.MinExchangeIndex <= 0 {
		return nil
	}
	if board == nil {
		return fmt.Errorf("task %s grant requires min exchange index %d but no blackboard is attached",
			grant.TaskID, grant.ContextPolicy.MinExchangeIndex)
	}
	if len(board.Exchanges) < grant.ContextPolicy.MinExchangeIndex {
		return fmt.Errorf("task %s grant requires at least %d exchanges but board has %d — stale context",
			grant.TaskID, grant.ContextPolicy.MinExchangeIndex, len(board.Exchanges))
	}
	return nil
}

func (r *Runtime) effectiveControlMode() ControlMode {
	switch r.controlMode {
	case ControlModeHybrid, ControlModeStrict:
		return r.controlMode
	default:
		return ControlModeLegacy
	}
}

// ErrStrictNoControl is returned when Strict mode is engaged but the
// RunState carries no ControlState — a caller started a team without
// bootstrapping control. Callers can recover by initializing the field.
var ErrStrictNoControl = errors.New("strict control mode requires RunState.Control to be initialized")
