package venat

import "github.com/Viking602/venat/api"

// Public sentinel errors are owned by api and re-exported here for callers
// that only need root-level construction and error checks.
var (
	ErrNotFound                  = api.ErrNotFound
	ErrTerminalState             = api.ErrTerminalState
	ErrStaleTaskVersion          = api.ErrStaleTaskVersion
	ErrLeaseHolderMismatch       = api.ErrLeaseHolderMismatch
	ErrLeaseNotActive            = api.ErrLeaseNotActive
	ErrOwnerMismatch             = api.ErrOwnerMismatch
	ErrActionTaskRequired        = api.ErrActionTaskRequired
	ErrActionReconcileRequired   = api.ErrActionReconcileRequired
	ErrIdempotencyConflict       = api.ErrIdempotencyConflict
	ErrResponseTaskRequired      = api.ErrResponseTaskRequired
	ErrResponsePublishInFlight   = api.ErrResponsePublishInFlight
	ErrPolicyDenied              = api.ErrPolicyDenied
	ErrPolicyObligationFailed    = api.ErrPolicyObligationFailed
	ErrDefinitionVersionConflict = api.ErrDefinitionVersionConflict
	ErrHandoffCycle              = api.ErrHandoffCycle
	ErrHandoffDepthExceeded      = api.ErrHandoffDepthExceeded
	ErrInvalidCommand            = api.ErrInvalidCommand
	ErrInvalidTransition         = api.ErrInvalidTransition
	ErrCompletionCriteriaUnmet   = api.ErrCompletionCriteriaUnmet
	ErrDependencyUnmet           = api.ErrDependencyUnmet
	ErrDependencyFailed          = api.ErrDependencyFailed
	ErrCheckpointLimitExceeded   = api.ErrCheckpointLimitExceeded
	ErrUsageUnpriced             = api.ErrUsageUnpriced
	ErrInvalidConfiguration      = api.ErrInvalidConfiguration
	ErrCapabilityNameReserved    = api.ErrCapabilityNameReserved
	ErrInvalidAddress            = api.ErrInvalidAddress
	ErrNoRecipients              = api.ErrNoRecipients
	ErrSubscriptionClosed        = api.ErrSubscriptionClosed
	ErrWaitTimeout               = api.ErrWaitTimeout
)
