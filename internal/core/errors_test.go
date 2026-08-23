package core_test

import (
	"errors"
	"testing"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core"
)

// Verify that each package-level error var in internal/core is the same
// sentinel as the corresponding model error.
func TestErrorVarsMatchModel(t *testing.T) {
	cases := []struct {
		name     string
		coreErr  error
		modelErr error
	}{
		{"ErrNotFound", core.ErrNotFound, api.ErrNotFound},
		{"ErrTerminalState", core.ErrTerminalState, api.ErrTerminalState},
		{"ErrStaleTaskVersion", core.ErrStaleTaskVersion, api.ErrStaleTaskVersion},
		{"ErrLeaseHolderMismatch", core.ErrLeaseHolderMismatch, api.ErrLeaseHolderMismatch},
		{"ErrLeaseNotActive", core.ErrLeaseNotActive, api.ErrLeaseNotActive},
		{"ErrOwnerMismatch", core.ErrOwnerMismatch, api.ErrOwnerMismatch},
		{"ErrActionTaskRequired", core.ErrActionTaskRequired, api.ErrActionTaskRequired},
		{"ErrActionReconcileRequired", core.ErrActionReconcileRequired, api.ErrActionReconcileRequired},
		{"ErrResponseTaskRequired", core.ErrResponseTaskRequired, api.ErrResponseTaskRequired},
		{"ErrPolicyDenied", core.ErrPolicyDenied, api.ErrPolicyDenied},
		{"ErrPolicyObligationFailed", core.ErrPolicyObligationFailed, api.ErrPolicyObligationFailed},
		{"ErrHandoffCycle", core.ErrHandoffCycle, api.ErrHandoffCycle},
		{"ErrHandoffDepthExceeded", core.ErrHandoffDepthExceeded, api.ErrHandoffDepthExceeded},
		{"ErrInvalidCommand", core.ErrInvalidCommand, api.ErrInvalidCommand},
		{"ErrInvalidTransition", core.ErrInvalidTransition, api.ErrInvalidTransition},
		{"ErrCompletionCriteriaUnmet", core.ErrCompletionCriteriaUnmet, api.ErrCompletionCriteriaUnmet},
		{"ErrDependencyUnmet", core.ErrDependencyUnmet, api.ErrDependencyUnmet},
		{"ErrDependencyFailed", core.ErrDependencyFailed, api.ErrDependencyFailed},
		{"ErrInvalidConfiguration", core.ErrInvalidConfiguration, api.ErrInvalidConfiguration},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.coreErr, tc.modelErr) {
				t.Errorf("core.%s is not the same sentinel as api.%s", tc.name, tc.name)
			}
		})
	}
}
