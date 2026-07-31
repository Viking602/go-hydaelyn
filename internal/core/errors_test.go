package core_test

import (
	"errors"
	"testing"

	"github.com/Viking602/venat/internal/core"
	"github.com/Viking602/venat/internal/core/model"
)

// Verify that each package-level error var in internal/core is the same
// sentinel as the corresponding model error.
func TestErrorVarsMatchModel(t *testing.T) {
	cases := []struct {
		name     string
		coreErr  error
		modelErr error
	}{
		{"ErrNotFound", core.ErrNotFound, model.ErrNotFound},
		{"ErrTerminalState", core.ErrTerminalState, model.ErrTerminalState},
		{"ErrStaleTaskVersion", core.ErrStaleTaskVersion, model.ErrStaleTaskVersion},
		{"ErrLeaseHolderMismatch", core.ErrLeaseHolderMismatch, model.ErrLeaseHolderMismatch},
		{"ErrLeaseNotActive", core.ErrLeaseNotActive, model.ErrLeaseNotActive},
		{"ErrOwnerMismatch", core.ErrOwnerMismatch, model.ErrOwnerMismatch},
		{"ErrActionTaskRequired", core.ErrActionTaskRequired, model.ErrActionTaskRequired},
		{"ErrActionReconcileRequired", core.ErrActionReconcileRequired, model.ErrActionReconcileRequired},
		{"ErrResponseTaskRequired", core.ErrResponseTaskRequired, model.ErrResponseTaskRequired},
		{"ErrPolicyDenied", core.ErrPolicyDenied, model.ErrPolicyDenied},
		{"ErrPolicyObligationFailed", core.ErrPolicyObligationFailed, model.ErrPolicyObligationFailed},
		{"ErrHandoffCycle", core.ErrHandoffCycle, model.ErrHandoffCycle},
		{"ErrHandoffDepthExceeded", core.ErrHandoffDepthExceeded, model.ErrHandoffDepthExceeded},
		{"ErrInvalidCommand", core.ErrInvalidCommand, model.ErrInvalidCommand},
		{"ErrInvalidTransition", core.ErrInvalidTransition, model.ErrInvalidTransition},
		{"ErrCompletionCriteriaUnmet", core.ErrCompletionCriteriaUnmet, model.ErrCompletionCriteriaUnmet},
		{"ErrDependencyUnmet", core.ErrDependencyUnmet, model.ErrDependencyUnmet},
		{"ErrDependencyFailed", core.ErrDependencyFailed, model.ErrDependencyFailed},
		{"ErrInvalidConfiguration", core.ErrInvalidConfiguration, model.ErrInvalidConfiguration},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.coreErr, tc.modelErr) {
				t.Errorf("core.%s is not the same sentinel as model.%s", tc.name, tc.name)
			}
		})
	}
}
