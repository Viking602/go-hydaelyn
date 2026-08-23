package venat

import (
	"context"

	"github.com/Viking602/venat/api"
)

// AcquireResourceClaims atomically acquires every requested opaque key or none.
func (r *Runner) AcquireResourceClaims(ctx context.Context, request api.ResourceClaimRequest) (api.ResourceClaimDecision, error) {
	decision, err := r.rt.AcquireResourceClaims(ctx, request)
	return decision, err
}

// TransitionResourceClaims atomically applies expected-version claim changes.
func (r *Runner) TransitionResourceClaims(ctx context.Context, request api.ResourceClaimTransitionRequest) (api.ResourceClaimDecision, error) {
	decision, err := r.rt.TransitionResourceClaims(ctx, request)
	return decision, err
}

// LoadResourceClaim loads one durable resource claim.
func (r *Runner) LoadResourceClaim(ctx context.Context, id string) (api.ResourceClaim, error) {
	claim, err := r.rt.LoadResourceClaim(ctx, id)
	return claim, err
}

// ListResourceClaims lists durable claims matching selector.
func (r *Runner) ListResourceClaims(ctx context.Context, selector api.ResourceClaimSelector) ([]api.ResourceClaim, error) {
	claims, err := r.rt.ListResourceClaims(ctx, selector)
	if err != nil {
		return nil, err
	}
	return claims, nil
}
