package core

import "github.com/Viking602/go-hydaelyn/internal/core/model"

// Package-level error vars re-exported from model so that internal/core files
// can reference them without an explicit model import on every call site.
var (
	ErrNotFound                = model.ErrNotFound
	ErrTerminalState           = model.ErrTerminalState
	ErrStaleTaskVersion        = model.ErrStaleTaskVersion
	ErrLeaseHolderMismatch     = model.ErrLeaseHolderMismatch
	ErrLeaseNotActive          = model.ErrLeaseNotActive
	ErrOwnerMismatch           = model.ErrOwnerMismatch
	ErrActionTaskRequired      = model.ErrActionTaskRequired
	ErrActionReconcileRequired = model.ErrActionReconcileRequired
	ErrResponseTaskRequired    = model.ErrResponseTaskRequired
	ErrPolicyDenied            = model.ErrPolicyDenied
	ErrPolicyObligationFailed  = model.ErrPolicyObligationFailed
	ErrFlowBypass              = model.ErrFlowBypass
	ErrHandoffCycle            = model.ErrHandoffCycle
	ErrHandoffDepthExceeded    = model.ErrHandoffDepthExceeded
	ErrInvalidCommand          = model.ErrInvalidCommand
	ErrInvalidTransition       = model.ErrInvalidTransition
	ErrCompletionCriteriaUnmet = model.ErrCompletionCriteriaUnmet
	ErrDependencyUnmet         = model.ErrDependencyUnmet
	ErrDependencyFailed        = model.ErrDependencyFailed
	ErrInvalidConfiguration    = model.ErrInvalidConfiguration
)
