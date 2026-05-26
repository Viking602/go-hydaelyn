package hydaelyn

import "github.com/Viking602/go-hydaelyn/api"

// Public sentinel errors are owned by api and re-exported here for callers
// that only need root-level construction and error checks.
var (
	ErrNotFound                = api.ErrNotFound
	ErrTerminalState           = api.ErrTerminalState
	ErrStaleTaskVersion        = api.ErrStaleTaskVersion
	ErrLeaseHolderMismatch     = api.ErrLeaseHolderMismatch
	ErrLeaseNotActive          = api.ErrLeaseNotActive
	ErrOwnerMismatch           = api.ErrOwnerMismatch
	ErrActionTaskRequired      = api.ErrActionTaskRequired
	ErrActionReconcileRequired = api.ErrActionReconcileRequired
	ErrIdempotencyConflict     = api.ErrIdempotencyConflict
	ErrResponseTaskRequired    = api.ErrResponseTaskRequired
	ErrPolicyDenied            = api.ErrPolicyDenied
	ErrPolicyObligationFailed  = api.ErrPolicyObligationFailed
	ErrHandoffCycle            = api.ErrHandoffCycle
	ErrHandoffDepthExceeded    = api.ErrHandoffDepthExceeded
	ErrInvalidCommand          = api.ErrInvalidCommand
	ErrInvalidTransition       = api.ErrInvalidTransition
	ErrCompletionCriteriaUnmet = api.ErrCompletionCriteriaUnmet
	ErrDependencyUnmet         = api.ErrDependencyUnmet
	ErrDependencyFailed        = api.ErrDependencyFailed
	ErrInvalidConfiguration    = api.ErrInvalidConfiguration
	ErrInvalidAddress          = api.ErrInvalidAddress
	ErrNoRecipients            = api.ErrNoRecipients
	ErrSubscriptionClosed      = api.ErrSubscriptionClosed
	ErrWaitTimeout             = api.ErrWaitTimeout
)
