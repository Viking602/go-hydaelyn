package core

import (
	"context"
	"fmt"
	"time"

	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
)

func (r *Runtime) AcquireResourceClaims(ctx context.Context, request model.ResourceClaimRequest) (model.ResourceClaimDecision, error) {
	request, err := normalizeResourceClaimRequest(request)
	if err != nil {
		return model.ResourceClaimDecision{}, err
	}
	uow, err := r.beginWriteUoW(ctx)
	if err != nil {
		return model.ResourceClaimDecision{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = uow.Rollback(ctx)
		}
	}()
	store, err := r.resourceClaimStore(ctx, uow)
	if err != nil {
		return model.ResourceClaimDecision{}, err
	}
	decision, err := store.AcquireResourceClaims(ctx, request)
	if err != nil {
		return model.ResourceClaimDecision{}, err
	}
	if err := uow.Commit(ctx); err != nil {
		return model.ResourceClaimDecision{}, err
	}
	committed = true
	return decision, nil
}

func (r *Runtime) TransitionResourceClaims(ctx context.Context, request model.ResourceClaimTransitionRequest) (model.ResourceClaimDecision, error) {
	request, err := normalizeResourceClaimTransitionRequest(request)
	if err != nil {
		return model.ResourceClaimDecision{}, err
	}
	uow, err := r.beginWriteUoW(ctx)
	if err != nil {
		return model.ResourceClaimDecision{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = uow.Rollback(ctx)
		}
	}()
	store, err := r.resourceClaimStore(ctx, uow)
	if err != nil {
		return model.ResourceClaimDecision{}, err
	}
	decision, err := store.TransitionResourceClaims(ctx, request)
	if err != nil {
		return model.ResourceClaimDecision{}, err
	}
	if err := uow.Commit(ctx); err != nil {
		return model.ResourceClaimDecision{}, err
	}
	committed = true
	return decision, nil
}

func normalizeResourceClaimRequest(request model.ResourceClaimRequest) (model.ResourceClaimRequest, error) {
	if request.RequestedAt.IsZero() || request.ExpiresAt.IsZero() || !request.ExpiresAt.After(request.RequestedAt) {
		return request, fmt.Errorf("resource claim timestamps are invalid: %w", model.ErrInvalidCommand)
	}
	ttl := request.ExpiresAt.Sub(request.RequestedAt)
	now := time.Now().UTC()
	request.RequestedAt = now
	request.ExpiresAt = now.Add(ttl)
	return request, nil
}

func normalizeResourceClaimTransitionRequest(request model.ResourceClaimTransitionRequest) (model.ResourceClaimTransitionRequest, error) {
	for _, transition := range request.Transitions {
		if transition.At.IsZero() {
			return request, fmt.Errorf("resource claim transition timestamp is invalid: %w", model.ErrInvalidTransition)
		}
		if transition.To == model.ResourceClaimActive {
			if transition.ExpiresAt.IsZero() || !transition.ExpiresAt.After(transition.At) {
				return request, fmt.Errorf("resource claim renewal expiry is invalid: %w", model.ErrInvalidTransition)
			}
		} else if !transition.ExpiresAt.IsZero() {
			return request, fmt.Errorf("terminal resource claim transition cannot set expiry: %w", model.ErrInvalidTransition)
		}
	}
	now := time.Now().UTC()
	for index := range request.Transitions {
		transition := &request.Transitions[index]
		ttl := transition.ExpiresAt.Sub(transition.At)
		transition.At = now
		if transition.To == model.ResourceClaimActive {
			transition.ExpiresAt = now.Add(ttl)
		}
	}
	return request, nil
}

func (r *Runtime) LoadResourceClaim(ctx context.Context, id string) (model.ResourceClaim, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return model.ResourceClaim{}, err
	}
	defer func() { _ = done() }()
	store, err := r.resourceClaimStore(ctx, uow)
	if err != nil {
		return model.ResourceClaim{}, err
	}
	return store.LoadResourceClaim(ctx, id)
}

func (r *Runtime) ListResourceClaims(ctx context.Context, selector model.ResourceClaimSelector) ([]model.ResourceClaim, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = done() }()
	store, err := r.resourceClaimStore(ctx, uow)
	if err != nil {
		return nil, err
	}
	return store.ListResourceClaims(ctx, selector)
}

func (r *Runtime) resourceClaimStore(ctx context.Context, uow ports.UnitOfWork) (ports.ResourceClaimStore, error) {
	capabilities, err := r.StoreCapabilities(ctx)
	if err != nil {
		return nil, err
	}
	if !capabilities.SupportsResourceClaims || !capabilities.SupportsTransactions {
		return nil, fmt.Errorf("resource claim storage requires transactional support: %w", model.ErrInvalidConfiguration)
	}
	extension, ok := uow.(ports.ResourceClaimUnitOfWork)
	if !ok || extension.ResourceClaims() == nil {
		return nil, fmt.Errorf("provider advertises resource claims without exposing the store: %w", model.ErrInvalidConfiguration)
	}
	return extension.ResourceClaims(), nil
}
