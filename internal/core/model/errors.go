package model

import "errors"

var (
	ErrNotFound                = errors.New("orchestrator: not found")
	ErrTerminalState           = errors.New("orchestrator: terminal state")
	ErrStaleTaskVersion        = errors.New("orchestrator: stale task version")
	ErrLeaseHolderMismatch     = errors.New("orchestrator: lease holder mismatch")
	ErrLeaseNotActive          = errors.New("orchestrator: lease not active")
	ErrOwnerMismatch           = errors.New("orchestrator: owner mismatch")
	ErrActionTaskRequired      = errors.New("orchestrator: action task required")
	ErrActionReconcileRequired = errors.New("orchestrator: action reconcile required")
	ErrResponseTaskRequired    = errors.New("orchestrator: response task required")
	ErrPolicyDenied            = errors.New("orchestrator: policy denied")
	ErrPolicyObligationFailed  = errors.New("orchestrator: policy obligation failed")
	ErrHandoffCycle            = errors.New("orchestrator: handoff owner cycle")
	ErrHandoffDepthExceeded    = errors.New("orchestrator: handoff depth exceeded")
	ErrInvalidCommand          = errors.New("orchestrator: invalid command")
	ErrInvalidTransition       = errors.New("orchestrator: invalid state transition")
	ErrCompletionCriteriaUnmet = errors.New("orchestrator: completion criteria unmet")
	ErrDependencyUnmet         = errors.New("orchestrator: dependency unmet")
	ErrDependencyFailed        = errors.New("orchestrator: dependency failed")
	ErrInvalidConfiguration    = errors.New("orchestrator: invalid configuration")
)
