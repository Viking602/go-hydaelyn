package adapter

import (
	"errors"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core"
	"github.com/Viking602/venat/internal/core/model"
)

type bridgedError struct {
	original error
	target   error
}

func (e bridgedError) Error() string   { return e.original.Error() }
func (e bridgedError) Unwrap() []error { return []error{e.original, e.target} }

func ErrorToAPI(err error) error {
	return bridgeError(err, coreToAPIErrors)
}

func ErrorToCore(err error) error {
	return bridgeError(err, apiToCoreErrors)
}

func bridgeError(err error, pairs []errorPair) error {
	if err == nil {
		return nil
	}
	for _, pair := range pairs {
		if errors.Is(err, pair.from) {
			if errors.Is(err, pair.to) {
				return err
			}
			return bridgedError{original: err, target: pair.to}
		}
	}
	return err
}

type errorPair struct {
	from error
	to   error
}

var coreToAPIErrors = []errorPair{
	{model.ErrNotFound, api.ErrNotFound},
	{model.ErrTerminalState, api.ErrTerminalState},
	{model.ErrStaleTaskVersion, api.ErrStaleTaskVersion},
	{model.ErrLeaseHolderMismatch, api.ErrLeaseHolderMismatch},
	{model.ErrLeaseNotActive, api.ErrLeaseNotActive},
	{model.ErrOwnerMismatch, api.ErrOwnerMismatch},
	{model.ErrActionTaskRequired, api.ErrActionTaskRequired},
	{model.ErrActionReconcileRequired, api.ErrActionReconcileRequired},
	{model.ErrIdempotencyConflict, api.ErrIdempotencyConflict},
	{model.ErrResponseTaskRequired, api.ErrResponseTaskRequired},
	{model.ErrPolicyDenied, api.ErrPolicyDenied},
	{model.ErrPolicyObligationFailed, api.ErrPolicyObligationFailed},
	{model.ErrDefinitionVersionConflict, api.ErrDefinitionVersionConflict},
	{model.ErrHandoffCycle, api.ErrHandoffCycle},
	{model.ErrHandoffDepthExceeded, api.ErrHandoffDepthExceeded},
	{model.ErrInvalidCommand, api.ErrInvalidCommand},
	{model.ErrInvalidTransition, api.ErrInvalidTransition},
	{model.ErrCompletionCriteriaUnmet, api.ErrCompletionCriteriaUnmet},
	{model.ErrDependencyUnmet, api.ErrDependencyUnmet},
	{model.ErrDependencyFailed, api.ErrDependencyFailed},
	{model.ErrCheckpointLimitExceeded, api.ErrCheckpointLimitExceeded},
	{model.ErrUsageUnpriced, api.ErrUsageUnpriced},
	{model.ErrInvalidConfiguration, api.ErrInvalidConfiguration},
	{model.ErrInvalidAddress, api.ErrInvalidAddress},
	{model.ErrNoRecipients, api.ErrNoRecipients},
	{core.ErrSubscriptionClosed, api.ErrSubscriptionClosed},
	{core.ErrWaitTimeout, api.ErrWaitTimeout},
}

var apiToCoreErrors = []errorPair{
	{api.ErrNotFound, model.ErrNotFound},
	{api.ErrTerminalState, model.ErrTerminalState},
	{api.ErrStaleTaskVersion, model.ErrStaleTaskVersion},
	{api.ErrLeaseHolderMismatch, model.ErrLeaseHolderMismatch},
	{api.ErrLeaseNotActive, model.ErrLeaseNotActive},
	{api.ErrOwnerMismatch, model.ErrOwnerMismatch},
	{api.ErrActionTaskRequired, model.ErrActionTaskRequired},
	{api.ErrActionReconcileRequired, model.ErrActionReconcileRequired},
	{api.ErrIdempotencyConflict, model.ErrIdempotencyConflict},
	{api.ErrResponseTaskRequired, model.ErrResponseTaskRequired},
	{api.ErrPolicyDenied, model.ErrPolicyDenied},
	{api.ErrPolicyObligationFailed, model.ErrPolicyObligationFailed},
	{api.ErrDefinitionVersionConflict, model.ErrDefinitionVersionConflict},
	{api.ErrHandoffCycle, model.ErrHandoffCycle},
	{api.ErrHandoffDepthExceeded, model.ErrHandoffDepthExceeded},
	{api.ErrInvalidCommand, model.ErrInvalidCommand},
	{api.ErrInvalidTransition, model.ErrInvalidTransition},
	{api.ErrCompletionCriteriaUnmet, model.ErrCompletionCriteriaUnmet},
	{api.ErrDependencyUnmet, model.ErrDependencyUnmet},
	{api.ErrDependencyFailed, model.ErrDependencyFailed},
	{api.ErrCheckpointLimitExceeded, model.ErrCheckpointLimitExceeded},
	{api.ErrUsageUnpriced, model.ErrUsageUnpriced},
	{api.ErrInvalidConfiguration, model.ErrInvalidConfiguration},
	{api.ErrInvalidAddress, model.ErrInvalidAddress},
	{api.ErrNoRecipients, model.ErrNoRecipients},
	{api.ErrSubscriptionClosed, core.ErrSubscriptionClosed},
	{api.ErrWaitTimeout, core.ErrWaitTimeout},
}
