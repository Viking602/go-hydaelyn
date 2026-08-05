package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Viking602/venat/internal/core/model"
)

type resourceClaimStore UnitOfWork

func (s *resourceClaimStore) ensureOpen() error {
	return (*UnitOfWork)(s).ensureOpen()
}

func (s *resourceClaimStore) AcquireResourceClaims(_ context.Context, request model.ResourceClaimRequest) (model.ResourceClaimDecision, error) {
	if err := s.ensureOpen(); err != nil {
		return model.ResourceClaimDecision{}, err
	}
	if err := validateResourceClaimRequest(request); err != nil {
		return model.ResourceClaimDecision{}, err
	}

	requestedIDs := make(map[string]struct{}, len(request.Claims))
	claims := make([]model.ResourceClaim, 0, len(request.Claims))
	for _, spec := range request.Claims {
		requestedIDs[spec.ID] = struct{}{}
		if existing, ok := s.staged.ResourceClaims[spec.ID]; ok {
			if !resourceClaimRequestMatches(existing, request, spec) {
				return model.ResourceClaimDecision{}, fmt.Errorf("resource claim %q: %w", spec.ID, model.ErrIdempotencyConflict)
			}
			claims = append(claims, existing)
			continue
		}
		claims = append(claims, model.ResourceClaim{
			ID: spec.ID, Key: spec.Key, Mode: spec.Mode,
			RunID: request.RunID, TaskID: request.TaskID, LeaseID: request.LeaseID, HolderID: request.HolderID,
			State: model.ResourceClaimActive, Version: 1,
			CreatedAt: request.RequestedAt, UpdatedAt: request.RequestedAt, ExpiresAt: request.ExpiresAt,
		})
	}

	conflicts := make([]model.ResourceClaim, 0)
	for _, existing := range s.staged.ResourceClaims {
		if _, ownRequest := requestedIDs[existing.ID]; ownRequest {
			continue
		}
		if existing.State != model.ResourceClaimActive || !existing.ExpiresAt.After(request.RequestedAt) {
			continue
		}
		for _, requested := range request.Claims {
			if existing.Key == requested.Key && (existing.Mode == model.ResourceClaimExclusive || requested.Mode == model.ResourceClaimExclusive) {
				conflicts = append(conflicts, existing)
				break
			}
		}
	}
	if len(conflicts) > 0 {
		sortResourceClaims(conflicts)
		return model.ResourceClaimDecision{
			Acquired: false, Reason: model.ResourceClaimDeniedConflict, Conflicts: conflicts,
		}, nil
	}

	for _, claim := range claims {
		if _, exists := s.staged.ResourceClaims[claim.ID]; !exists {
			s.staged.ResourceClaims[claim.ID] = claim
		}
	}
	return model.ResourceClaimDecision{Acquired: true, Claims: claims}, nil
}

func (s *resourceClaimStore) TransitionResourceClaims(_ context.Context, request model.ResourceClaimTransitionRequest) (model.ResourceClaimDecision, error) {
	if err := s.ensureOpen(); err != nil {
		return model.ResourceClaimDecision{}, err
	}
	if len(request.Transitions) == 0 {
		return model.ResourceClaimDecision{}, fmt.Errorf("resource claim transitions are required: %w", model.ErrInvalidCommand)
	}

	claims, versionConflicts, err := prepareResourceClaimTransitions(
		s.staged.ResourceClaims,
		request.Transitions,
	)
	if err != nil {
		return model.ResourceClaimDecision{}, err
	}
	if len(versionConflicts) > 0 {
		sortResourceClaims(versionConflicts)
		return model.ResourceClaimDecision{
			Acquired: false, Reason: model.ResourceClaimDeniedVersionConflict, Conflicts: versionConflicts,
		}, nil
	}

	candidates := resourceClaimTransitionCandidates(s.staged.ResourceClaims, request.Transitions)
	conflicts := resourceClaimTransitionConflicts(candidates, request.Transitions)
	if len(conflicts) > 0 {
		sortResourceClaims(conflicts)
		return model.ResourceClaimDecision{Acquired: false, Reason: model.ResourceClaimDeniedConflict, Conflicts: conflicts}, nil
	}

	for index, transition := range request.Transitions {
		claim := candidates[transition.ClaimID]
		s.staged.ResourceClaims[claim.ID] = claim
		claims[index] = claim
	}
	return model.ResourceClaimDecision{Acquired: true, Claims: claims}, nil
}

func prepareResourceClaimTransitions(
	current map[string]model.ResourceClaim,
	transitions []model.ResourceClaimTransition,
) ([]model.ResourceClaim, []model.ResourceClaim, error) {
	seen := make(map[string]struct{}, len(transitions))
	claims := make([]model.ResourceClaim, len(transitions))
	versionConflicts := make([]model.ResourceClaim, 0)
	for index, transition := range transitions {
		if strings.TrimSpace(transition.ClaimID) == "" {
			return nil, nil, fmt.Errorf("resource claim ID is required: %w", model.ErrInvalidCommand)
		}
		if _, duplicate := seen[transition.ClaimID]; duplicate {
			return nil, nil, fmt.Errorf("duplicate resource claim transition %q: %w", transition.ClaimID, model.ErrInvalidCommand)
		}
		seen[transition.ClaimID] = struct{}{}
		claim, ok := current[transition.ClaimID]
		if !ok {
			return nil, nil, model.ErrNotFound
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
	current map[string]model.ResourceClaim,
	transitions []model.ResourceClaimTransition,
) map[string]model.ResourceClaim {
	candidates := make(map[string]model.ResourceClaim, len(current))
	for id, claim := range current {
		candidates[id] = claim
	}
	for _, transition := range transitions {
		claim := candidates[transition.ClaimID]
		claim.State = transition.To
		claim.Version++
		claim.UpdatedAt = transition.At
		if transition.To == model.ResourceClaimActive {
			claim.ExpiresAt = transition.ExpiresAt
		}
		candidates[claim.ID] = claim
	}
	return candidates
}

func resourceClaimTransitionConflicts(
	candidates map[string]model.ResourceClaim,
	transitions []model.ResourceClaimTransition,
) []model.ResourceClaim {
	conflicts := make([]model.ResourceClaim, 0)
	conflictIDs := make(map[string]struct{})
	for _, transition := range transitions {
		if transition.To != model.ResourceClaimActive {
			continue
		}
		requested := candidates[transition.ClaimID]
		for id, existing := range candidates {
			if id == requested.ID || existing.State != model.ResourceClaimActive || !existing.ExpiresAt.After(transition.At) {
				continue
			}
			if existing.Key != requested.Key || (existing.Mode != model.ResourceClaimExclusive && requested.Mode != model.ResourceClaimExclusive) {
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

func (s *resourceClaimStore) LoadResourceClaim(_ context.Context, id string) (model.ResourceClaim, error) {
	if err := s.ensureOpen(); err != nil {
		return model.ResourceClaim{}, err
	}
	claim, ok := s.staged.ResourceClaims[id]
	if !ok {
		return model.ResourceClaim{}, model.ErrNotFound
	}
	return claim, nil
}

func (s *resourceClaimStore) ListResourceClaims(_ context.Context, selector model.ResourceClaimSelector) ([]model.ResourceClaim, error) {
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}
	claims := make([]model.ResourceClaim, 0)
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

func validateResourceClaimRequest(request model.ResourceClaimRequest) error {
	if request.RunID == "" || request.TaskID == "" || request.LeaseID == "" || request.HolderID == "" {
		return fmt.Errorf("resource claim run, task, lease, and holder IDs are required: %w", model.ErrInvalidCommand)
	}
	if request.RequestedAt.IsZero() || !request.ExpiresAt.After(request.RequestedAt) {
		return fmt.Errorf("resource claim timestamps are invalid: %w", model.ErrInvalidCommand)
	}
	if len(request.Claims) == 0 {
		return fmt.Errorf("resource claims are required: %w", model.ErrInvalidCommand)
	}
	ids := make(map[string]struct{}, len(request.Claims))
	keys := make(map[string]struct{}, len(request.Claims))
	for _, claim := range request.Claims {
		if strings.TrimSpace(claim.ID) == "" || strings.TrimSpace(claim.Key) == "" {
			return fmt.Errorf("resource claim ID and key are required: %w", model.ErrInvalidCommand)
		}
		if claim.Mode != model.ResourceClaimShared && claim.Mode != model.ResourceClaimExclusive {
			return fmt.Errorf("resource claim %q has invalid mode %q: %w", claim.ID, claim.Mode, model.ErrInvalidCommand)
		}
		if _, duplicate := ids[claim.ID]; duplicate {
			return fmt.Errorf("duplicate resource claim ID %q: %w", claim.ID, model.ErrInvalidCommand)
		}
		ids[claim.ID] = struct{}{}
		if _, duplicate := keys[claim.Key]; duplicate {
			return fmt.Errorf("duplicate resource claim key %q: %w", claim.Key, model.ErrInvalidCommand)
		}
		keys[claim.Key] = struct{}{}
	}
	return nil
}

func validateResourceClaimTransition(claim model.ResourceClaim, transition model.ResourceClaimTransition) error {
	if transition.At.IsZero() || transition.At.Before(claim.UpdatedAt) {
		return fmt.Errorf("resource claim transition timestamp is invalid: %w", model.ErrInvalidTransition)
	}
	if claim.State != model.ResourceClaimActive {
		return fmt.Errorf("resource claim %q is terminal: %w", claim.ID, model.ErrInvalidTransition)
	}
	switch transition.To {
	case model.ResourceClaimActive:
		if !claim.ExpiresAt.After(transition.At) {
			return fmt.Errorf("resource claim %q has expired: %w", claim.ID, model.ErrInvalidTransition)
		}
		if !transition.ExpiresAt.After(transition.At) {
			return fmt.Errorf("resource claim renewal expiry is invalid: %w", model.ErrInvalidTransition)
		}
	case model.ResourceClaimReleased, model.ResourceClaimExpired:
		if !transition.ExpiresAt.IsZero() {
			return fmt.Errorf("terminal resource claim transition cannot set expiry: %w", model.ErrInvalidTransition)
		}
	default:
		return fmt.Errorf("resource claim transition to %q: %w", transition.To, model.ErrInvalidTransition)
	}
	return nil
}

func resourceClaimRequestMatches(claim model.ResourceClaim, request model.ResourceClaimRequest, spec model.ResourceClaimSpec) bool {
	return claim.ID == spec.ID && claim.Key == spec.Key && claim.Mode == spec.Mode &&
		claim.RunID == request.RunID && claim.TaskID == request.TaskID && claim.LeaseID == request.LeaseID &&
		claim.HolderID == request.HolderID && claim.State == model.ResourceClaimActive &&
		claim.ExpiresAt.After(request.RequestedAt) &&
		claim.ExpiresAt.Sub(claim.CreatedAt) == request.ExpiresAt.Sub(request.RequestedAt)
}

func matchesResourceClaimSelector(claim model.ResourceClaim, selector model.ResourceClaimSelector) bool {
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

func containsResourceClaimMode(values []model.ResourceClaimMode, target model.ResourceClaimMode) bool {
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

func containsResourceClaimState(values []model.ResourceClaimState, target model.ResourceClaimState) bool {
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

func sortResourceClaims(claims []model.ResourceClaim) {
	sort.Slice(claims, func(left, right int) bool {
		if claims[left].Key == claims[right].Key {
			return claims[left].ID < claims[right].ID
		}
		return claims[left].Key < claims[right].Key
	})
}
