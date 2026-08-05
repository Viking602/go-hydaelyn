package venat

import (
	"context"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/adapter"
)

// AcquireResourceClaims atomically acquires every requested opaque key or none.
func (r *Runner) AcquireResourceClaims(ctx context.Context, request api.ResourceClaimRequest) (api.ResourceClaimDecision, error) {
	decision, err := r.rt.AcquireResourceClaims(ctx, adapter.ResourceClaimRequestToModel(request))
	return adapter.ResourceClaimDecisionFromModel(decision), adapter.ErrorToAPI(err)
}

// TransitionResourceClaims atomically applies expected-version claim changes.
func (r *Runner) TransitionResourceClaims(ctx context.Context, request api.ResourceClaimTransitionRequest) (api.ResourceClaimDecision, error) {
	decision, err := r.rt.TransitionResourceClaims(ctx, adapter.ResourceClaimTransitionRequestToModel(request))
	return adapter.ResourceClaimDecisionFromModel(decision), adapter.ErrorToAPI(err)
}

// LoadResourceClaim loads one durable resource claim.
func (r *Runner) LoadResourceClaim(ctx context.Context, id string) (api.ResourceClaim, error) {
	claim, err := r.rt.LoadResourceClaim(ctx, id)
	return adapter.ResourceClaimFromModel(claim), adapter.ErrorToAPI(err)
}

// ListResourceClaims lists durable claims matching selector.
func (r *Runner) ListResourceClaims(ctx context.Context, selector api.ResourceClaimSelector) ([]api.ResourceClaim, error) {
	claims, err := r.rt.ListResourceClaims(ctx, adapter.ResourceClaimSelectorToModel(selector))
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return adapter.ResourceClaimsFromModel(claims), nil
}
