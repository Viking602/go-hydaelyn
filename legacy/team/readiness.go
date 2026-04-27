package team

import (
	"fmt"

	"github.com/Viking602/go-hydaelyn/internal/blackboard"
)

// BlockerKind classifies why a pending task is not yet runnable.
type BlockerKind string

const (
	// BlockerDependencyPending — a prerequisite task is still pending/running.
	BlockerDependencyPending BlockerKind = "dependency_pending"
	// BlockerDependencyFailed — a prerequisite task ended in a non-completed
	// terminal state (failed/aborted/skipped). Surfaced for replan/audit, even
	// though dependent tasks already get aborted by abortDependentTasks.
	BlockerDependencyFailed BlockerKind = "dependency_failed"
	// BlockerDependencyMissing — a declared dependency id does not exist in
	// the task list. Indicates a plan integrity bug upstream.
	BlockerDependencyMissing BlockerKind = "dependency_missing"
	// BlockerReadMissing — a declared Reads key has no matching blackboard
	// exchange yet. The task would start blind without its declared inputs.
	BlockerReadMissing BlockerKind = "read_missing"
)

// Blocker describes a single concrete reason a task is not runnable.
type Blocker struct {
	Kind   BlockerKind `json:"kind"`
	Target string      `json:"target,omitempty"`
	Reason string      `json:"reason,omitempty"`
}

// RunnableCandidate carries a pending task together with its readiness
// assessment. Ready candidates have no blockers and are safe to dispatch.
// Non-ready candidates enumerate concrete blockers so a supervisor layer can
// decide whether to wait, replan, or request more evidence.
type RunnableCandidate struct {
	Task     Task      `json:"task"`
	Ready    bool      `json:"ready"`
	Blockers []Blocker `json:"blockers,omitempty"`
}

// RunnableCandidates inspects every pending task and reports its dispatch
// readiness. Dependencies are checked against the current task list;
// declared Reads are checked against the blackboard when one is attached.
//
// This is an additive helper — the existing RunStateRunnableTasks path keeps
// its original semantics (deps-only, returns only runnable tasks). Callers
// that want structured blocker reasons or stricter gating should consume
// RunnableCandidates instead.
func (s RunState) RunnableCandidates() []RunnableCandidate {
	current := s
	current.Normalize()

	statusByTask := make(map[string]TaskStatus, len(current.Tasks))
	for _, task := range current.Tasks {
		statusByTask[task.ID] = task.Status
	}

	candidates := make([]RunnableCandidate, 0, len(current.Tasks))
	for _, task := range current.Tasks {
		if task.Status != TaskStatusPending {
			continue
		}
		blockers := collectBlockers(task, statusByTask, current.Blackboard)
		candidates = append(candidates, RunnableCandidate{
			Task:     task,
			Ready:    len(blockers) == 0,
			Blockers: blockers,
		})
	}
	return candidates
}

func collectBlockers(task Task, statusByTask map[string]TaskStatus, board *blackboard.State) []Blocker {
	var blockers []Blocker
	for _, dep := range task.DependsOn {
		status, exists := statusByTask[dep]
		switch {
		case !exists:
			blockers = append(blockers, Blocker{
				Kind:   BlockerDependencyMissing,
				Target: dep,
				Reason: fmt.Sprintf("dependency %s not declared in task list", dep),
			})
		case status == TaskStatusCompleted:
			// Dependency satisfied — no blocker.
		case isDependencyFailureStatus(status):
			blockers = append(blockers, Blocker{
				Kind:   BlockerDependencyFailed,
				Target: dep,
				Reason: fmt.Sprintf("dependency %s ended with status %s", dep, status),
			})
		default:
			blockers = append(blockers, Blocker{
				Kind:   BlockerDependencyPending,
				Target: dep,
				Reason: fmt.Sprintf("dependency %s is %s", dep, status),
			})
		}
	}
	if len(task.ReadSelectors) > 0 {
		for _, sel := range task.ReadSelectors {
			if !sel.Required {
				continue
			}
			if !selectorSatisfied(board, sel) {
				blockers = append(blockers, Blocker{
					Kind:   BlockerReadMissing,
					Target: selectorLabel(sel),
					Reason: fmt.Sprintf("no blackboard exchange satisfies required selector %q", selectorLabel(sel)),
				})
			}
		}
	} else {
		for _, key := range task.Reads {
			if !readSatisfied(board, key) {
				blockers = append(blockers, Blocker{
					Kind:   BlockerReadMissing,
					Target: key,
					Reason: fmt.Sprintf("no blackboard exchange available for read %q", key),
				})
			}
		}
	}
	return blockers
}

func selectorSatisfied(board *blackboard.State, sel blackboard.ExchangeSelector) bool {
	if board == nil {
		return false
	}
	if len(board.SelectExchanges(sel)) > 0 {
		return true
	}
	if sel.RequireVerified && len(board.SelectFindings(sel)) > 0 {
		return true
	}
	return false
}

func selectorLabel(sel blackboard.ExchangeSelector) string {
	if sel.Label != "" {
		return sel.Label
	}
	if len(sel.Keys) > 0 {
		return sel.Keys[0]
	}
	if len(sel.Namespaces) > 0 {
		return sel.Namespaces[0]
	}
	return "selector"
}

func isDependencyFailureStatus(status TaskStatus) bool {
	switch status {
	case TaskStatusFailed, TaskStatusAborted, TaskStatusSkipped:
		return true
	default:
		return false
	}
}

// readSatisfied treats a read as available when any exchange matches the key.
// An absent blackboard means declared reads cannot possibly be satisfied, so
// tasks with Reads block until a blackboard is wired — which matches the
// intent of declaring them in the first place. Tasks without Reads are never
// blocked by this check.
func readSatisfied(board *blackboard.State, key string) bool {
	if board == nil {
		return false
	}
	return len(board.ExchangesForKey(key)) > 0
}
