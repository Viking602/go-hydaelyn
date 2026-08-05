package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Viking602/venat"
	"github.com/Viking602/venat/api"
)

// ResourceClaimRecoveryController reconciles expired resource claims after a
// process restart.
type ResourceClaimRecoveryController interface {
	RecoverExpiredResourceClaims(context.Context, time.Time, int) ([]api.ResourceClaim, error)
}

// StandardResourceClaimController uses Runner's expected-version claim store.
type StandardResourceClaimController struct {
	Runner *venat.Runner
	Now    func() time.Time
}

// RecoverExpiredResourceClaims marks due active claims expired. Concurrent
// lifecycle winners are skipped rather than overwritten.
func (c StandardResourceClaimController) RecoverExpiredResourceClaims(ctx context.Context, at time.Time, limit int) ([]api.ResourceClaim, error) {
	if c.Runner == nil {
		return nil, ErrRunnerMissing
	}
	if at.IsZero() {
		if c.Now != nil {
			at = c.Now().UTC()
		} else {
			at = time.Now().UTC()
		}
	} else {
		at = at.UTC()
	}
	candidates, err := c.Runner.ListResourceClaims(ctx, api.ResourceClaimSelector{
		States: []api.ResourceClaimState{api.ResourceClaimActive}, ExpiresBefore: at, Limit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("worker: list expired resource claims: %w", err)
	}
	recovered := make([]api.ResourceClaim, 0, len(candidates))
	for _, candidate := range candidates {
		transitionAt := at
		if transitionAt.Before(candidate.UpdatedAt) {
			transitionAt = candidate.UpdatedAt
		}
		decision, transitionErr := c.Runner.TransitionResourceClaims(ctx, api.ResourceClaimTransitionRequest{
			Transitions: []api.ResourceClaimTransition{{
				ClaimID: candidate.ID, ExpectedVersion: candidate.Version,
				To: api.ResourceClaimExpired, At: transitionAt,
			}},
		})
		if transitionErr != nil {
			if errors.Is(transitionErr, api.ErrNotFound) || errors.Is(transitionErr, api.ErrInvalidTransition) {
				continue
			}
			return recovered, transitionErr
		}
		if !decision.Acquired || len(decision.Claims) != 1 {
			continue
		}
		recovered = append(recovered, decision.Claims[0])
	}
	return recovered, nil
}
