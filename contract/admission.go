package contract

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Viking602/venat/api"
)

func runAdmissionReservationSuite(t *testing.T, factory ProviderFactory) {
	t.Helper()
	probe := newProvider(t, factory)
	if !capabilities(t, probe).SupportsAdmissionReservations {
		t.Skip("provider does not advertise admission reservations")
	}
	runSuite(t, factory, []suiteCase{
		{"TestAdmissionReservation_LifecycleCAS", testAdmissionReservationLifecycleCAS},
		{"TestAdmissionReservation_AtomicConcurrentReserve", testAdmissionReservationAtomicConcurrentReserve},
		{"TestAdmissionReservation_CreditAndRunWindow", testAdmissionReservationCreditAndRunWindow},
		{"TestAdmissionReservation_FailureBreaker", testAdmissionReservationFailureBreaker},
	})
}

func testAdmissionReservationLifecycleCAS(t *testing.T, factory ProviderFactory) {
	provider := newProvider(t, factory)
	ctx := context.Background()
	now := time.Date(2026, time.August, 4, 8, 0, 0, 0, time.UTC)
	request := contractAdmissionRequest(
		"reservation-1", "run-1", now,
		api.AdmissionLimits{Window: time.Hour, MaxConcurrentRuns: 1}, 10,
	)
	preview, err := contractReserve(ctx, provider, request, true)
	requireAdmissionDecision(t, "preview", preview, err, true, "", "", 0)
	if preview.Reservation.ID != "" {
		t.Fatalf("preview persisted reservation=%#v", preview.Reservation)
	}
	listed, err := contractListAdmissions(ctx, provider, api.AdmissionReservationSelector{})
	if err != nil || len(listed) != 0 {
		t.Fatalf("preview mutated reservations=%#v error=%v", listed, err)
	}

	reserved, err := contractReserve(ctx, provider, request, false)
	requireAdmissionDecision(t, "reserve", reserved, err, true, "", api.AdmissionReserved, 1)
	stale, err := contractTransitionAdmission(ctx, provider, api.AdmissionTransition{
		ReservationID: request.ReservationID, ExpectedVersion: 0,
		To: api.AdmissionActive, At: now.Add(time.Minute),
	})
	requireAdmissionDecision(t, "stale transition", stale, err, false, api.AdmissionDeniedVersionConflict, "", 0)
	active, err := contractTransitionAdmission(ctx, provider, api.AdmissionTransition{
		ReservationID: request.ReservationID, ExpectedVersion: 1,
		To: api.AdmissionActive, At: now.Add(time.Minute),
	})
	requireAdmissionDecision(t, "activate", active, err, true, "", api.AdmissionActive, 2)
	suspended, err := contractTransitionAdmission(ctx, provider, api.AdmissionTransition{
		ReservationID: request.ReservationID, ExpectedVersion: 2,
		To: api.AdmissionSuspended, At: now.Add(2 * time.Minute),
	})
	requireAdmissionDecision(t, "suspend", suspended, err, true, "", api.AdmissionSuspended, 3)

	second := contractAdmissionRequest("reservation-2", "run-2", now.Add(3*time.Minute), request.Limits, 10)
	secondDecision, err := contractReserve(ctx, provider, second, false)
	requireAdmissionDecision(t, "reserve after suspension", secondDecision, err, true, "", api.AdmissionReserved, 1)
	resume, err := contractTransitionAdmission(ctx, provider, api.AdmissionTransition{
		ReservationID: request.ReservationID, ExpectedVersion: 3,
		To: api.AdmissionActive, At: now.Add(4 * time.Minute),
	})
	requireAdmissionDecision(t, "resume", resume, err, false, api.AdmissionDeniedConcurrency, "", 0)
}

func requireAdmissionDecision(
	t *testing.T,
	label string,
	decision api.AdmissionDecision,
	err error,
	allowed bool,
	reason api.AdmissionDenialReason,
	state api.AdmissionState,
	version uint64,
) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s error=%v", label, err)
	}
	if decision.Allowed != allowed {
		t.Fatalf("%s allowed=%t, want %t: %#v", label, decision.Allowed, allowed, decision)
	}
	if reason != "" && decision.Reason != reason {
		t.Fatalf("%s reason=%q, want %q: %#v", label, decision.Reason, reason, decision)
	}
	if state != "" && decision.Reservation.State != state {
		t.Fatalf("%s state=%q, want %q: %#v", label, decision.Reservation.State, state, decision)
	}
	if version != 0 && decision.Reservation.Version != version {
		t.Fatalf("%s version=%d, want %d: %#v", label, decision.Reservation.Version, version, decision)
	}
}

func testAdmissionReservationAtomicConcurrentReserve(t *testing.T, factory ProviderFactory) {
	provider := newProvider(t, factory)
	const contenders = 24
	now := time.Date(2026, time.August, 4, 9, 0, 0, 0, time.UTC)
	limits := api.AdmissionLimits{MaxConcurrentRuns: 1}
	start := make(chan struct{})
	errorsCh := make(chan error, contenders)
	var allowed atomic.Int64
	var wait sync.WaitGroup
	wait.Add(contenders)
	for index := range contenders {
		go func() {
			defer wait.Done()
			<-start
			request := contractAdmissionRequest(fmt.Sprintf("reservation-%02d", index), fmt.Sprintf("run-%02d", index), now, limits, 0)
			decision, err := contractReserve(context.Background(), provider, request, false)
			if err != nil {
				errorsCh <- err
				return
			}
			if decision.Allowed {
				allowed.Add(1)
			}
		}()
	}
	close(start)
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Errorf("concurrent reserve: %v", err)
	}
	if got := allowed.Load(); got != 1 {
		t.Fatalf("concurrent allowed count = %d, want 1", got)
	}
	listed, err := contractListAdmissions(context.Background(), provider, api.AdmissionReservationSelector{})
	if err != nil || len(listed) != 1 {
		t.Fatalf("persisted concurrent winners=%#v error=%v", listed, err)
	}
}

func testAdmissionReservationCreditAndRunWindow(t *testing.T, factory ProviderFactory) {
	provider := newProvider(t, factory)
	ctx := context.Background()
	now := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
	creditLimits := api.AdmissionLimits{Window: time.Hour, MaxCredits: 50}
	first, err := contractReserve(ctx, provider, contractAdmissionRequest("credit-1", "credit-run-1", now, creditLimits, 30), false)
	if err != nil || !first.Allowed {
		t.Fatalf("first credit reserve decision=%#v error=%v", first, err)
	}
	second, err := contractReserve(ctx, provider, contractAdmissionRequest("credit-2", "credit-run-2", now.Add(time.Minute), creditLimits, 30), false)
	if err != nil || second.Allowed || second.Reason != api.AdmissionDeniedCredits {
		t.Fatalf("second credit reserve decision=%#v error=%v", second, err)
	}

	windowLimits := api.AdmissionLimits{Window: time.Hour, MaxRunsPerWindow: 1}
	windowFirstRequest := contractAdmissionRequest("window-1", "window-run-1", now, windowLimits, 0)
	windowFirstRequest.AgentID = "window-agent"
	windowFirst, err := contractReserve(ctx, provider, windowFirstRequest, false)
	if err != nil || !windowFirst.Allowed {
		t.Fatalf("first window reserve decision=%#v error=%v", windowFirst, err)
	}
	windowSecondRequest := contractAdmissionRequest("window-2", "window-run-2", now.Add(time.Minute), windowLimits, 0)
	windowSecondRequest.AgentID = "window-agent"
	windowSecond, err := contractReserve(ctx, provider, windowSecondRequest, false)
	if err != nil || windowSecond.Allowed || windowSecond.Reason != api.AdmissionDeniedRunWindow {
		t.Fatalf("second window reserve decision=%#v error=%v", windowSecond, err)
	}
}

func testAdmissionReservationFailureBreaker(t *testing.T, factory ProviderFactory) {
	provider := newProvider(t, factory)
	ctx := context.Background()
	now := time.Date(2026, time.August, 4, 11, 0, 0, 0, time.UTC)
	limits := api.AdmissionLimits{Window: time.Hour, PauseOnExcessFailures: 1}
	request := contractAdmissionRequest("failure-1", "failure-run-1", now, limits, 0)
	reserved, err := contractReserve(ctx, provider, request, false)
	if err != nil || !reserved.Allowed {
		t.Fatalf("failure reserve decision=%#v error=%v", reserved, err)
	}
	active, err := contractTransitionAdmission(ctx, provider, api.AdmissionTransition{
		ReservationID: request.ReservationID, ExpectedVersion: 1, To: api.AdmissionActive, At: now.Add(time.Minute),
	})
	if err != nil || !active.Allowed {
		t.Fatalf("failure activate decision=%#v error=%v", active, err)
	}
	settled, err := contractTransitionAdmission(ctx, provider, api.AdmissionTransition{
		ReservationID: request.ReservationID, ExpectedVersion: 2, To: api.AdmissionSettled, At: now.Add(2 * time.Minute), Failed: true,
	})
	if err != nil || !settled.Allowed {
		t.Fatalf("failure settle decision=%#v error=%v", settled, err)
	}
	blocked, err := contractReserve(ctx, provider, contractAdmissionRequest("failure-2", "failure-run-2", now.Add(3*time.Minute), limits, 0), false)
	if err != nil || blocked.Allowed || blocked.Reason != api.AdmissionDeniedFailureBreaker {
		t.Fatalf("breaker decision=%#v error=%v", blocked, err)
	}
}

func contractAdmissionRequest(id, runID string, now time.Time, limits api.AdmissionLimits, credits int64) api.AdmissionRequest {
	return api.AdmissionRequest{
		ReservationID: id, AgentID: "contract-agent", RunID: runID, Limits: limits,
		ReservedCredits: credits, RequestedAt: now, ExpiresAt: now.Add(30 * time.Minute),
	}
}

func contractReserve(ctx context.Context, provider api.StoreProvider, request api.AdmissionRequest, preview bool) (api.AdmissionDecision, error) {
	uow, err := provider.Begin(ctx)
	if err != nil {
		return api.AdmissionDecision{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = uow.Rollback(ctx)
		}
	}()
	store, err := contractAdmissionStore(uow)
	if err != nil {
		return api.AdmissionDecision{}, err
	}
	var decision api.AdmissionDecision
	if preview {
		decision, err = store.PreviewAdmission(ctx, request)
	} else {
		decision, err = store.ReserveAdmission(ctx, request)
	}
	if err != nil {
		return api.AdmissionDecision{}, err
	}
	if err := uow.Commit(ctx); err != nil {
		return api.AdmissionDecision{}, err
	}
	committed = true
	return decision, nil
}

func contractTransitionAdmission(ctx context.Context, provider api.StoreProvider, transition api.AdmissionTransition) (api.AdmissionDecision, error) {
	uow, err := provider.Begin(ctx)
	if err != nil {
		return api.AdmissionDecision{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = uow.Rollback(ctx)
		}
	}()
	store, err := contractAdmissionStore(uow)
	if err != nil {
		return api.AdmissionDecision{}, err
	}
	decision, err := store.TransitionAdmission(ctx, transition)
	if err != nil {
		return api.AdmissionDecision{}, err
	}
	if err := uow.Commit(ctx); err != nil {
		return api.AdmissionDecision{}, err
	}
	committed = true
	return decision, nil
}

func contractListAdmissions(ctx context.Context, provider api.StoreProvider, selector api.AdmissionReservationSelector) ([]api.AdmissionReservation, error) {
	uow, err := provider.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = uow.Rollback(ctx) }()
	store, err := contractAdmissionStore(uow)
	if err != nil {
		return nil, err
	}
	return store.ListAdmissionReservations(ctx, selector)
}

func contractAdmissionStore(uow api.UnitOfWork) (api.AdmissionReservationStore, error) {
	extension, ok := uow.(api.AdmissionReservationUnitOfWork)
	if !ok || extension.AdmissionReservations() == nil {
		return nil, fmt.Errorf("provider advertises admission reservations without exposing api.AdmissionReservationUnitOfWork")
	}
	return extension.AdmissionReservations(), nil
}
