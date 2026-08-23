package core

import "github.com/Viking602/venat/api"

// Package-level error vars re-exported from model so that internal/core files
// can reference them without an explicit model import on every call site.
var (
	ErrNotFound                = api.ErrNotFound
	ErrTerminalState           = api.ErrTerminalState
	ErrStaleTaskVersion        = api.ErrStaleTaskVersion
	ErrLeaseHolderMismatch     = api.ErrLeaseHolderMismatch
	ErrLeaseNotActive          = api.ErrLeaseNotActive
	ErrOwnerMismatch           = api.ErrOwnerMismatch
	ErrActionTaskRequired      = api.ErrActionTaskRequired
	ErrActionReconcileRequired = api.ErrActionReconcileRequired
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
)
