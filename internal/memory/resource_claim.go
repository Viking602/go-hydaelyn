package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Viking602/venat/api"
)

type resourceClaimStore UnitOfWork

func (s *resourceClaimStore) ensureOpen() error {
	return (*UnitOfWork)(s).ensureOpen()
}

func (s *resourceClaimStore) AcquireResourceClaims(_ context.Context, request api.ResourceClaimRequest) (api.ResourceClaimDecision, error) {
	if err := s.ensureOpen(); err != nil {
		return api.ResourceClaimDecision{}, err
	}
	if err := validateResourceClaimRequest(request); err != nil {
		return api.ResourceClaimDecision{}, err
	}

	requestedIDs := make(map[string]struct{}, len(request.Claims))
	claims := make([]api.ResourceClaim, 0, len(request.Claims))
	for _, spec := range request.Claims {
		requestedIDs[spec.ID] = struct{}{}
		if existing, ok := s.staged.ResourceClaims[spec.ID]; ok {
			if !resourceClaimRequestMatches(existing, request, spec) {
				return api.ResourceClaimDecision{}, fmt.Errorf("resource claim %q: %w", spec.ID, api.ErrIdempotencyConflict)
			}
			claims = append(claims, existing)
			continue
		}
		claims = append(claims, api.ResourceClaim{
			ID: spec.ID, Key: spec.Key, Mode: spec.Mode,
			RunID: request.RunID, TaskID: request.TaskID, LeaseID: request.LeaseID, HolderID: request.HolderID,
			State: api.ResourceClaimActive, Version: 1,
			CreatedAt: request.RequestedAt, UpdatedAt: request.RequestedAt, ExpiresAt: request.ExpiresAt,
		})
	}

	conflicts := make([]api.ResourceClaim, 0)
	for _, existing := range s.staged.ResourceClaims {
		if _, ownRequest := requestedIDs[existing.ID]; ownRequest {
			continue
		}
		if existing.State != api.ResourceClaimActive || !existing.ExpiresAt.After(request.RequestedAt) {
			continue
		}
		for _, requested := range request.Claims {
			if existing.Key == requested.Key && (existing.Mode == api.ResourceClaimExclusive || requested.Mode == api.ResourceClaimExclusive) {
				conflicts = append(conflicts, existing)
				break
			}
		}
	}
	if len(conflicts) > 0 {
		sortResourceClaims(conflicts)
		return api.ResourceClaimDecision{
			Acquired: false, Reason: api.ResourceClaimDeniedConflict, Conflicts: conflicts,
		}, nil
	}

	for _, claim := range claims {
		if _, exists := s.staged.ResourceClaims[claim.ID]; !exists {
			s.staged.ResourceClaims[claim.ID] = claim
		}
	}
	return api.ResourceClaimDecision{Acquired: true, Claims: claims}, nil
}

func (s *resourceClaimStore) TransitionResourceClaims(_ context.Context, request api.ResourceClaimTransitionRequest) (api.ResourceClaimDecision, error) {
	if err := s.ensureOpen(); err != nil {
		return api.ResourceClaimDecision{}, err
	}
	if len(request.Transitions) == 0 {
		return api.ResourceClaimDecision{}, fmt.Errorf("resource claim transitions are required: %w", api.ErrInvalidCommand)
	}

	claims, versionConflicts, err := prepareResourceClaimTransitions(
		s.staged.ResourceClaims,
		request.Transitions,
	)
	if err != nil {
		return api.ResourceClaimDecision{}, err
	}
	if len(versionConflicts) > 0 {
		sortResourceClaims(versionConflicts)
		return api.ResourceClaimDecision{
			Acquired: false, Reason: api.ResourceClaimDeniedVersionConflict, Conflicts: versionConflicts,
		}, nil
	}

	candidates := resourceClaimTransitionCandidates(s.staged.ResourceClaims, request.Transitions)
	conflicts := resourceClaimTransitionConflicts(candidates, request.Transitions)
	if len(conflicts) > 0 {
		sortResourceClaims(conflicts)
		return api.ResourceClaimDecision{Acquired: false, Reason: api.ResourceClaimDeniedConflict, Conflicts: conflicts}, nil
	}

	for index, transition := range request.Transitions {
		claim := candidates[transition.ClaimID]
		s.staged.ResourceClaims[claim.ID] = claim
		claims[index] = claim
	}
	return api.ResourceClaimDecision{Acquired: true, Claims: claims}, nil
}

func prepareResourceClaimTransitions(
	current map[string]api.ResourceClaim,
	transitions []api.ResourceClaimTransition,
) ([]api.ResourceClaim, []api.ResourceClaim, error) {
	seen := make(map[string]struct{}, len(transitions))
	claims := make([]api.ResourceClaim, len(transitions))
	versionConflicts := make([]api.ResourceClaim, 0)
	for index, transition := range transitions {
		if strings.TrimSpace(transition.ClaimID) == "" {
			return nil, nil, fmt.Errorf("resource claim ID is required: %w", api.ErrInvalidCommand)
		}
		if _, duplicate := seen[transition.ClaimID]; duplicate {
			return nil, nil, fmt.Errorf("duplicate resource claim transition %q: %w", transition.ClaimID, api.ErrInvalidCommand)
		}
		seen[transition.ClaimID] = struct{}{}
		claim, ok := current[transition.ClaimID]
		if !ok {
			return nil, nil, api.ErrNotFound
		}
		claims[index] = claim
		if claim.Version != transition.ExpectedVersion {
			versionConflicts = append(versionConflicts, claim)
			continue
		}
		if err := validateResourceClaimTransition(claim, transition); err != nil {
			return nil, nil, err
		}
	}
	return claims, versionConflicts, nil
}

func resourceClaimTransitionCandidates(
	current map[string]api.ResourceClaim,
	transitions []api.ResourceClaimTransition,
) map[string]api.ResourceClaim {
	candidates := make(map[string]api.ResourceClaim, len(current))
	for id, claim := range current {
		candidates[id] = claim
	}
	for _, transition := range transitions {
		claim := candidates[transition.ClaimID]
		claim.State = transition.To
		claim.Version++
		claim.UpdatedAt = transition.At
		if transition.To == api.ResourceClaimActive {
			claim.ExpiresAt = transition.ExpiresAt
		}
		candidates[claim.ID] = claim
	}
	return candidates
}

func resourceClaimTransitionConflicts(
	candidates map[string]api.ResourceClaim,
	transitions []api.ResourceClaimTransition,
) []api.ResourceClaim {
	conflicts := make([]api.ResourceClaim, 0)
	conflictIDs := make(map[string]struct{})
	for _, transition := range transitions {
		if transition.To != api.ResourceClaimActive {
			continue
		}
		requested := candidates[transition.ClaimID]
		for id, existing := range candidates {
			if id == requested.ID || existing.State != api.ResourceClaimActive || !existing.ExpiresAt.After(transition.At) {
				continue
			}
			if existing.Key != requested.Key || (existing.Mode != api.ResourceClaimExclusive && requested.Mode != api.ResourceClaimExclusive) {
				continue
			}
			if _, duplicate := conflictIDs[id]; duplicate {
				continue
			}
			conflictIDs[id] = struct{}{}
			conflicts = append(conflicts, existing)
		}
	}
	return conflicts
}

func (s *resourceClaimStore) LoadResourceClaim(_ context.Context, id string) (api.ResourceClaim, error) {
	if err := s.ensureOpen(); err != nil {
		return api.ResourceClaim{}, err
	}
	claim, ok := s.staged.ResourceClaims[id]
	if !ok {
		return api.ResourceClaim{}, api.ErrNotFound
	}
	return claim, nil
}

func (s *resourceClaimStore) ListResourceClaims(_ context.Context, selector api.ResourceClaimSelector) ([]api.ResourceClaim, error) {
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}
	claims := make([]api.ResourceClaim, 0)
	for _, claim := range s.staged.ResourceClaims {
		if matchesResourceClaimSelector(claim, selector) {
			claims = append(claims, claim)
		}
	}
	sortResourceClaims(claims)
	if selector.Limit > 0 && len(claims) > selector.Limit {
		claims = claims[:selector.Limit]
	}
	return claims, nil
}

func validateResourceClaimRequest(request api.ResourceClaimRequest) error {
	if request.RunID == "" || request.TaskID == "" || request.LeaseID == "" || request.HolderID == "" {
		return fmt.Errorf("resource claim run, task, lease, and holder IDs are required: %w", api.ErrInvalidCommand)
	}
	if request.RequestedAt.IsZero() || !request.ExpiresAt.After(request.RequestedAt) {
		return fmt.Errorf("resource claim timestamps are invalid: %w", api.ErrInvalidCommand)
	}
	if len(request.Claims) == 0 {
		return fmt.Errorf("resource claims are required: %w", api.ErrInvalidCommand)
	}
	ids := make(map[string]struct{}, len(request.Claims))
	keys := make(map[string]struct{}, len(request.Claims))
	for _, claim := range request.Claims {
		if strings.TrimSpace(claim.ID) == "" || strings.TrimSpace(claim.Key) == "" {
			return fmt.Errorf("resource claim ID and key are required: %w", api.ErrInvalidCommand)
		}
		if claim.Mode != api.ResourceClaimShared && claim.Mode != api.ResourceClaimExclusive {
			return fmt.Errorf("resource claim %q has invalid mode %q: %w", claim.ID, claim.Mode, api.ErrInvalidCommand)
		}
		if _, duplicate := ids[claim.ID]; duplicate {
			return fmt.Errorf("duplicate resource claim ID %q: %w", claim.ID, api.ErrInvalidCommand)
		}
		ids[claim.ID] = struct{}{}
		if _, duplicate := keys[claim.Key]; duplicate {
			return fmt.Errorf("duplicate resource claim key %q: %w", claim.Key, api.ErrInvalidCommand)
		}
		keys[claim.Key] = struct{}{}
	}
	return nil
}

func validateResourceClaimTransition(claim api.ResourceClaim, transition api.ResourceClaimTransition) error {
	if transition.At.IsZero() || transition.At.Before(claim.UpdatedAt) {
		return fmt.Errorf("resource claim transition timestamp is invalid: %w", api.ErrInvalidTransition)
	}
	if claim.State != api.ResourceClaimActive {
		return fmt.Errorf("resource claim %q is terminal: %w", claim.ID, api.ErrInvalidTransition)
	}
	switch transition.To {
	case api.ResourceClaimActive:
		if !claim.ExpiresAt.After(transition.At) {
			return fmt.Errorf("resource claim %q has expired: %w", claim.ID, api.ErrInvalidTransition)
		}
		if !transition.ExpiresAt.After(transition.At) {
			return fmt.Errorf("resource claim renewal expiry is invalid: %w", api.ErrInvalidTransition)
		}
	case api.ResourceClaimReleased, api.ResourceClaimExpired:
		if !transition.ExpiresAt.IsZero() {
			return fmt.Errorf("terminal resource claim transition cannot set expiry: %w", api.ErrInvalidTransition)
		}
	default:
		return fmt.Errorf("resource claim transition to %q: %w", transition.To, api.ErrInvalidTransition)
	}
	return nil
}

func resourceClaimRequestMatches(claim api.ResourceClaim, request api.ResourceClaimRequest, spec api.ResourceClaimSpec) bool {
	return claim.ID == spec.ID && claim.Key == spec.Key && claim.Mode == spec.Mode &&
		claim.RunID == request.RunID && claim.TaskID == request.TaskID && claim.LeaseID == request.LeaseID &&
		claim.HolderID == request.HolderID && claim.State == api.ResourceClaimActive &&
		claim.ExpiresAt.After(request.RequestedAt) &&
		claim.ExpiresAt.Sub(claim.CreatedAt) == request.ExpiresAt.Sub(request.RequestedAt)
}

func matchesResourceClaimSelector(claim api.ResourceClaim, selector api.ResourceClaimSelector) bool {
	return containsString(selector.IDs, claim.ID) && containsString(selector.Keys, claim.Key) &&
		containsString(selector.RunIDs, claim.RunID) && containsString(selector.TaskIDs, claim.TaskID) &&
		containsString(selector.LeaseIDs, claim.LeaseID) && containsString(selector.HolderIDs, claim.HolderID) &&
		containsResourceClaimMode(selector.Modes, claim.Mode) && containsResourceClaimState(selector.States, claim.State) &&
		(selector.ExpiresBefore.IsZero() || !claim.ExpiresAt.After(selector.ExpiresBefore))
}

func containsString(values []string, target string) bool {
	if len(values) == 0 {
		return true
	}
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsResourceClaimMode(values []api.ResourceClaimMode, target api.ResourceClaimMode) bool {
	if len(values) == 0 {
		return true
	}
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsResourceClaimState(values []api.ResourceClaimState, target api.ResourceClaimState) bool {
	if len(values) == 0 {
		return true
	}
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func sortResourceClaims(claims []api.ResourceClaim) {
	sort.Slice(claims, func(left, right int) bool {
		if claims[left].Key == claims[right].Key {
			return claims[left].ID < claims[right].ID
		}
		return claims[left].Key < claims[right].Key
	})
}
