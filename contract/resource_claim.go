package contract

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Viking602/venat/api"
)

func runResourceClaimSuite(t *testing.T, factory ProviderFactory) {
	t.Helper()
	probe := newProvider(t, factory)
	capabilities := capabilities(t, probe)
	if !capabilities.SupportsResourceClaims || !capabilities.SupportsTransactions {
		t.Skip("provider does not advertise transactional resource claims")
	}
	runSuite(t, factory, []suiteCase{
		{"TestResourceClaim_SharedExclusiveAndAtomicBatch", testResourceClaimSharedExclusiveAndAtomicBatch},
		{"TestResourceClaim_LifecycleBatchCAS", testResourceClaimLifecycleBatchCAS},
		{"TestResourceClaim_ExpiredRenewalCannotOverlap", testResourceClaimExpiredRenewalCannotOverlap},
		{"TestResourceClaim_AtomicConcurrentExclusive", testResourceClaimAtomicConcurrentExclusive},
	})
}

func testResourceClaimSharedExclusiveAndAtomicBatch(t *testing.T, factory ProviderFactory) {
	provider := newProvider(t, factory)
	ctx := context.Background()
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	first := contractClaimRequest("first", "lease-first", now,
		api.ResourceClaimSpec{ID: "claim-first", Key: "repo:alpha", Mode: api.ResourceClaimShared})
	firstDecision, err := contractAcquireClaims(ctx, provider, first)
	if err != nil || !firstDecision.Acquired || len(firstDecision.Claims) != 1 || firstDecision.Claims[0].Version != 1 {
		t.Fatalf("first shared decision=%#v error=%v", firstDecision, err)
	}
	second := contractClaimRequest("second", "lease-second", now,
		api.ResourceClaimSpec{ID: "claim-second", Key: "repo:alpha", Mode: api.ResourceClaimShared})
	secondDecision, err := contractAcquireClaims(ctx, provider, second)
	if err != nil || !secondDecision.Acquired {
		t.Fatalf("second shared decision=%#v error=%v", secondDecision, err)
	}

	blocked := contractClaimRequest("blocked", "lease-blocked", now,
		api.ResourceClaimSpec{ID: "claim-free", Key: "repo:free", Mode: api.ResourceClaimExclusive},
		api.ResourceClaimSpec{ID: "claim-blocked", Key: "repo:alpha", Mode: api.ResourceClaimExclusive})
	blockedDecision, err := contractAcquireClaims(ctx, provider, blocked)
	if err != nil || blockedDecision.Acquired || blockedDecision.Reason != api.ResourceClaimDeniedConflict || len(blockedDecision.Conflicts) != 2 {
		t.Fatalf("blocked batch decision=%#v error=%v", blockedDecision, err)
	}
	listed, err := contractListClaims(ctx, provider, api.ResourceClaimSelector{})
	if err != nil || len(listed) != 2 {
		t.Fatalf("atomic batch persisted partial claims=%#v error=%v", listed, err)
	}
	for _, claim := range listed {
		if claim.Key == "repo:free" {
			t.Fatalf("conflicted batch persisted free key: %#v", claim)
		}
	}
}

func testResourceClaimLifecycleBatchCAS(t *testing.T, factory ProviderFactory) {
	provider := newProvider(t, factory)
	ctx := context.Background()
	now := time.Date(2026, time.August, 4, 13, 0, 0, 0, time.UTC)
	request := contractClaimRequest("owner", "lease-owner", now,
		api.ResourceClaimSpec{ID: "claim-a", Key: "repo:a", Mode: api.ResourceClaimExclusive},
		api.ResourceClaimSpec{ID: "claim-b", Key: "repo:b", Mode: api.ResourceClaimShared})
	acquired, err := contractAcquireClaims(ctx, provider, request)
	if err != nil || !acquired.Acquired || len(acquired.Claims) != 2 {
		t.Fatalf("acquire decision=%#v error=%v", acquired, err)
	}
	stale, err := contractTransitionClaims(ctx, provider, api.ResourceClaimTransitionRequest{Transitions: []api.ResourceClaimTransition{
		{ClaimID: "claim-a", ExpectedVersion: 1, To: api.ResourceClaimReleased, At: now.Add(time.Minute)},
		{ClaimID: "claim-b", ExpectedVersion: 0, To: api.ResourceClaimReleased, At: now.Add(time.Minute)},
	}})
	if err != nil || stale.Acquired || stale.Reason != api.ResourceClaimDeniedVersionConflict || len(stale.Conflicts) != 1 {
		t.Fatalf("stale batch decision=%#v error=%v", stale, err)
	}
	unchanged, err := contractListClaims(ctx, provider, api.ResourceClaimSelector{States: []api.ResourceClaimState{api.ResourceClaimActive}})
	if err != nil || len(unchanged) != 2 || unchanged[0].Version != 1 || unchanged[1].Version != 1 {
		t.Fatalf("stale batch mutated claims=%#v error=%v", unchanged, err)
	}
	renewed, err := contractTransitionClaims(ctx, provider, api.ResourceClaimTransitionRequest{Transitions: []api.ResourceClaimTransition{
		{ClaimID: "claim-a", ExpectedVersion: 1, To: api.ResourceClaimActive, At: now.Add(time.Minute), ExpiresAt: now.Add(time.Hour)},
		{ClaimID: "claim-b", ExpectedVersion: 1, To: api.ResourceClaimActive, At: now.Add(time.Minute), ExpiresAt: now.Add(time.Hour)},
	}})
	if err != nil || !renewed.Acquired || len(renewed.Claims) != 2 || renewed.Claims[0].Version != 2 || renewed.Claims[1].Version != 2 {
		t.Fatalf("renew batch decision=%#v error=%v", renewed, err)
	}
}

func testResourceClaimExpiredRenewalCannotOverlap(t *testing.T, factory ProviderFactory) {
	ctx := context.Background()
	provider := newProvider(t, factory)
	start := time.Date(2026, time.August, 5, 15, 0, 0, 0, time.UTC)
	first := contractClaimRequest("expired-owner", "lease-expired", start,
		api.ResourceClaimSpec{ID: "claim-expired", Key: "repo:renew", Mode: api.ResourceClaimExclusive})
	first.ExpiresAt = start.Add(time.Minute)
	acquired, err := contractAcquireClaims(ctx, provider, first)
	if err != nil || !acquired.Acquired {
		t.Fatalf("first acquire decision=%#v error=%v", acquired, err)
	}
	current := start.Add(2 * time.Minute)
	second := contractClaimRequest("current-owner", "lease-current", current,
		api.ResourceClaimSpec{ID: "claim-current", Key: "repo:renew", Mode: api.ResourceClaimExclusive})
	if decision, err := contractAcquireClaims(ctx, provider, second); err != nil || !decision.Acquired {
		t.Fatalf("current acquire decision=%#v error=%v", decision, err)
	}
	staleRenewal, err := contractTransitionClaims(ctx, provider, api.ResourceClaimTransitionRequest{Transitions: []api.ResourceClaimTransition{{
		ClaimID: "claim-expired", ExpectedVersion: 1, To: api.ResourceClaimActive,
		At: start.Add(30 * time.Second), ExpiresAt: start.Add(10 * time.Minute),
	}}})
	if err != nil || staleRenewal.Acquired || staleRenewal.Reason != api.ResourceClaimDeniedConflict {
		t.Fatalf("renewal overlapping current claim decision=%#v error=%v", staleRenewal, err)
	}
	renewed, err := contractTransitionClaims(ctx, provider, api.ResourceClaimTransitionRequest{Transitions: []api.ResourceClaimTransition{{
		ClaimID: "claim-expired", ExpectedVersion: 1, To: api.ResourceClaimActive,
		At: current, ExpiresAt: current.Add(time.Minute),
	}}})
	if !errors.Is(err, api.ErrInvalidTransition) {
		t.Fatalf("expired renewal error=%v, want ErrInvalidTransition", err)
	}
	if renewed.Acquired {
		t.Fatalf("expired renewal decision=%#v, want rejected", renewed)
	}
}

func testResourceClaimAtomicConcurrentExclusive(t *testing.T, factory ProviderFactory) {
	provider := newProvider(t, factory)
	const contenders = 24
	now := time.Date(2026, time.August, 4, 14, 0, 0, 0, time.UTC)
	start := make(chan struct{})
	errorsCh := make(chan error, contenders)
	var winners atomic.Int64
	var wait sync.WaitGroup
	wait.Add(contenders)
	for index := range contenders {
		go func() {
			defer wait.Done()
			<-start
			id := fmt.Sprintf("contender-%02d", index)
			decision, err := contractAcquireClaims(context.Background(), provider, contractClaimRequest(id, "lease-"+id, now,
				api.ResourceClaimSpec{ID: "claim-" + id, Key: "repo:exclusive", Mode: api.ResourceClaimExclusive}))
			if err != nil {
				errorsCh <- err
				return
			}
			if decision.Acquired {
				winners.Add(1)
			}
		}()
	}
	close(start)
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Errorf("concurrent acquire: %v", err)
	}
	if got := winners.Load(); got != 1 {
		t.Fatalf("concurrent winner count = %d, want 1", got)
	}
	listed, err := contractListClaims(context.Background(), provider, api.ResourceClaimSelector{Keys: []string{"repo:exclusive"}})
	if err != nil || len(listed) != 1 {
		t.Fatalf("persisted concurrent winners=%#v error=%v", listed, err)
	}
}

func contractClaimRequest(owner, leaseID string, now time.Time, claims ...api.ResourceClaimSpec) api.ResourceClaimRequest {
	return api.ResourceClaimRequest{
		RunID: "run-" + owner, TaskID: "task-" + owner, LeaseID: leaseID, HolderID: owner,
		Claims: claims, RequestedAt: now, ExpiresAt: now.Add(30 * time.Minute),
	}
}

func contractAcquireClaims(ctx context.Context, provider api.StoreProvider, request api.ResourceClaimRequest) (api.ResourceClaimDecision, error) {
	uow, err := provider.Begin(ctx)
	if err != nil {
		return api.ResourceClaimDecision{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = uow.Rollback(ctx)
		}
	}()
	store, err := contractResourceClaimStore(uow)
	if err != nil {
		return api.ResourceClaimDecision{}, err
	}
	decision, err := store.AcquireResourceClaims(ctx, request)
	if err != nil {
		return api.ResourceClaimDecision{}, err
	}
	if err := uow.Commit(ctx); err != nil {
		return api.ResourceClaimDecision{}, err
	}
	committed = true
	return decision, nil
}

func contractTransitionClaims(ctx context.Context, provider api.StoreProvider, request api.ResourceClaimTransitionRequest) (api.ResourceClaimDecision, error) {
	uow, err := provider.Begin(ctx)
	if err != nil {
		return api.ResourceClaimDecision{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = uow.Rollback(ctx)
		}
	}()
	store, err := contractResourceClaimStore(uow)
	if err != nil {
		return api.ResourceClaimDecision{}, err
	}
	decision, err := store.TransitionResourceClaims(ctx, request)
	if err != nil {
		return api.ResourceClaimDecision{}, err
	}
	if err := uow.Commit(ctx); err != nil {
		return api.ResourceClaimDecision{}, err
	}
	committed = true
	return decision, nil
}

func contractListClaims(ctx context.Context, provider api.StoreProvider, selector api.ResourceClaimSelector) ([]api.ResourceClaim, error) {
	uow, err := provider.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = uow.Rollback(ctx) }()
	store, err := contractResourceClaimStore(uow)
	if err != nil {
		return nil, err
	}
	return store.ListResourceClaims(ctx, selector)
}

func contractResourceClaimStore(uow api.UnitOfWork) (api.ResourceClaimStore, error) {
	extension, ok := uow.(api.ResourceClaimUnitOfWork)
	if !ok || extension.ResourceClaims() == nil {
		return nil, fmt.Errorf("provider advertises resource claims without exposing api.ResourceClaimUnitOfWork")
	}
	return extension.ResourceClaims(), nil
}
