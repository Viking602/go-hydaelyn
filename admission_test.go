package venat

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/Viking602/venat/api"
)

func TestAdmissionReservationLifecycleAndCapacity(t *testing.T) {
	ctx := context.Background()
	runner := NewDevelopment()
	now := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
	limits := api.AdmissionLimits{Window: time.Hour, MaxConcurrentRuns: 1, MaxRunsPerWindow: 3, MaxCredits: 100}
	first := admissionRequest("reservation-1", "run-1", now, limits, 40)
	decision, err := runner.ReserveAdmission(ctx, first)
	if err != nil {
		t.Fatalf("ReserveAdmission(first) error = %v", err)
	}
	if !decision.Allowed || decision.Reservation.State != api.AdmissionReserved || decision.Reservation.Version != 1 {
		t.Fatalf("first decision = %#v", decision)
	}

	preview, err := runner.PreviewAdmission(ctx, admissionRequest("reservation-2", "run-2", now.Add(time.Minute), limits, 20))
	if err != nil {
		t.Fatalf("PreviewAdmission() error = %v", err)
	}
	if preview.Allowed || preview.Reason != api.AdmissionDeniedConcurrency || preview.Usage.ConcurrentRuns != 1 {
		t.Fatalf("preview decision = %#v", preview)
	}
	reservations, err := runner.ListAdmissionReservations(ctx, api.AdmissionReservationSelector{})
	if err != nil {
		t.Fatalf("ListAdmissionReservations() error = %v", err)
	}
	if len(reservations) != 1 {
		t.Fatalf("preview mutated reservations: %#v", reservations)
	}

	active, err := runner.TransitionAdmission(ctx, api.AdmissionTransition{
		ReservationID: "reservation-1", ExpectedVersion: 1, To: api.AdmissionActive, At: now.Add(2 * time.Minute),
	})
	if err != nil || !active.Allowed || active.Reservation.State != api.AdmissionActive {
		t.Fatalf("activate decision=%#v error=%v", active, err)
	}
	suspended, err := runner.TransitionAdmission(ctx, api.AdmissionTransition{
		ReservationID: "reservation-1", ExpectedVersion: 2, To: api.AdmissionSuspended, At: now.Add(3 * time.Minute),
	})
	if err != nil || !suspended.Allowed {
		t.Fatalf("suspend decision=%#v error=%v", suspended, err)
	}

	second := admissionRequest("reservation-2", "run-2", now.Add(4*time.Minute), limits, 20)
	secondDecision, err := runner.ReserveAdmission(ctx, second)
	if err != nil || !secondDecision.Allowed {
		t.Fatalf("ReserveAdmission(second) decision=%#v error=%v", secondDecision, err)
	}
	resume, err := runner.TransitionAdmission(ctx, api.AdmissionTransition{
		ReservationID: "reservation-1", ExpectedVersion: 3, To: api.AdmissionActive, At: now.Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("resume error = %v", err)
	}
	if resume.Allowed || resume.Reason != api.AdmissionDeniedConcurrency {
		t.Fatalf("resume decision = %#v, want concurrency denial", resume)
	}

	settled, err := runner.TransitionAdmission(ctx, api.AdmissionTransition{
		ReservationID: "reservation-2", ExpectedVersion: 1, To: api.AdmissionActive, At: now.Add(6 * time.Minute),
	})
	if err != nil || !settled.Allowed {
		t.Fatalf("activate second decision=%#v error=%v", settled, err)
	}
	if err := runner.AppendUsage(ctx, api.UsageRecord{
		ID: "priced-usage", RunID: "run-2", Kind: api.UsageKindModelCall,
		PricingState: api.UsagePricingStatePriced, Credits: 25,
	}); err != nil {
		t.Fatalf("AppendUsage() error = %v", err)
	}
	finishAdmissionAgentTask(ctx, t, runner, "run-2", api.TaskStatusCompleted)
	settled, err = runner.TransitionAdmission(ctx, api.AdmissionTransition{
		ReservationID: "reservation-2", ExpectedVersion: 2, To: api.AdmissionSettled,
		At: now.Add(7 * time.Minute), ConsumedCredits: 1, Failed: true,
	})
	if err != nil || !settled.Allowed || settled.Reservation.ConsumedCredits != 25 || settled.Reservation.Failed {
		t.Fatalf("settle second decision=%#v error=%v", settled, err)
	}
}

func TestAdmissionCallerCannotAdvanceQuotaClock(t *testing.T) {
	ctx := context.Background()
	runner := NewDevelopment()
	callerNow := time.Now().UTC().Add(24 * time.Hour)
	limits := api.AdmissionLimits{MaxConcurrentRuns: 1}

	before := time.Now().UTC()
	first, err := runner.ReserveAdmission(ctx, admissionRequest("trusted-time-1", "trusted-time-run-1", callerNow, limits, 0))
	after := time.Now().UTC()
	if err != nil || !first.Allowed {
		t.Fatalf("ReserveAdmission(first) decision=%#v error=%v", first, err)
	}
	if first.Reservation.CreatedAt.Before(before) || first.Reservation.CreatedAt.After(after) {
		t.Fatalf("reservation createdAt = %s, want runtime interval [%s, %s]", first.Reservation.CreatedAt, before, after)
	}
	if lifetime := first.Reservation.ExpiresAt.Sub(first.Reservation.CreatedAt); lifetime != 30*time.Minute {
		t.Fatalf("reservation lifetime = %s, want 30m", lifetime)
	}

	second, err := runner.PreviewAdmission(ctx, admissionRequest(
		"trusted-time-2", "trusted-time-run-2", callerNow.Add(time.Hour), limits, 0,
	))
	if err != nil {
		t.Fatalf("PreviewAdmission(second) error = %v", err)
	}
	if second.Allowed || second.Reason != api.AdmissionDeniedConcurrency || second.Usage.ConcurrentRuns != 1 {
		t.Fatalf("future-dated preview = %#v, want live concurrency denial", second)
	}
}

func TestAdmissionSettlementRejectsUnpricedUsage(t *testing.T) {
	ctx := context.Background()
	runner := NewDevelopment()
	now := time.Date(2026, time.August, 4, 11, 0, 0, 0, time.UTC)
	reserved, err := runner.ReserveAdmission(ctx, admissionRequest(
		"unpriced-reservation", "unpriced-run", now,
		api.AdmissionLimits{Window: time.Hour, MaxCredits: 100}, 10,
	))
	if err != nil || !reserved.Allowed {
		t.Fatalf("ReserveAdmission() decision=%#v error=%v", reserved, err)
	}
	if _, err := runner.TransitionAdmission(ctx, api.AdmissionTransition{
		ReservationID: "unpriced-reservation", ExpectedVersion: reserved.Reservation.Version,
		To: api.AdmissionActive, At: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("activate admission: %v", err)
	}
	if err := runner.AppendUsage(ctx, api.UsageRecord{
		ID: "unpriced-usage", RunID: "unpriced-run", Kind: api.UsageKindModelCall,
		PricingState: api.UsagePricingStateUnpriced,
	}); err != nil {
		t.Fatalf("AppendUsage() error = %v", err)
	}
	_, err = runner.TransitionAdmission(ctx, api.AdmissionTransition{
		ReservationID: "unpriced-reservation", ExpectedVersion: reserved.Reservation.Version + 1,
		To: api.AdmissionSettled, At: now.Add(2 * time.Minute), ConsumedCredits: 0,
	})
	if !errors.Is(err, api.ErrUsageUnpriced) {
		t.Fatalf("settlement error = %v, want ErrUsageUnpriced", err)
	}
	current, loadErr := runner.LoadAdmissionReservation(ctx, "unpriced-reservation")
	if loadErr != nil {
		t.Fatalf("LoadAdmissionReservation() error = %v", loadErr)
	}
	if current.State == api.AdmissionSettled {
		t.Fatalf("admission settled despite unpriced usage: %#v", current)
	}
}

func TestAdmissionSettlementUsesDurableFailureOutcome(t *testing.T) {
	ctx := context.Background()
	runner := NewDevelopment()
	now := time.Now().UTC()
	limits := api.AdmissionLimits{
		Window: time.Hour, MaxConcurrentRuns: 1, PauseOnExcessFailures: 1,
	}
	reserved, err := runner.ReserveAdmission(ctx, admissionRequest(
		"failed-reservation", "failed-run", now, limits, 0,
	))
	if err != nil || !reserved.Allowed {
		t.Fatalf("ReserveAdmission() decision=%#v error=%v", reserved, err)
	}
	active, err := runner.TransitionAdmission(ctx, api.AdmissionTransition{
		ReservationID: "failed-reservation", ExpectedVersion: reserved.Reservation.Version,
		To: api.AdmissionActive, At: now.Add(time.Minute),
	})
	if err != nil || !active.Allowed {
		t.Fatalf("activate admission decision=%#v error=%v", active, err)
	}
	_, err = runner.TransitionAdmission(ctx, api.AdmissionTransition{
		ReservationID: "failed-reservation", ExpectedVersion: active.Reservation.Version,
		To: api.AdmissionSettled, At: now.Add(2 * time.Minute),
	})
	if !errors.Is(err, api.ErrInvalidTransition) {
		t.Fatalf("early settlement error = %v, want ErrInvalidTransition", err)
	}

	finishAdmissionAgentTask(ctx, t, runner, "failed-run", api.TaskStatusFailed)
	settled, err := runner.TransitionAdmission(ctx, api.AdmissionTransition{
		ReservationID: "failed-reservation", ExpectedVersion: active.Reservation.Version,
		To: api.AdmissionSettled, At: now.Add(3 * time.Minute), Failed: false,
	})
	if err != nil || !settled.Allowed || !settled.Reservation.Failed {
		t.Fatalf("failed settlement decision=%#v error=%v", settled, err)
	}
	next, err := runner.PreviewAdmission(ctx, admissionRequest(
		"after-failure-reservation", "after-failure-run", now.Add(4*time.Minute), limits, 0,
	))
	if err != nil {
		t.Fatalf("PreviewAdmission() error = %v", err)
	}
	if next.Allowed || next.Reason != api.AdmissionDeniedFailureBreaker || next.Usage.TrailingFailures != 1 {
		t.Fatalf("post-failure decision = %#v, want failure-breaker denial", next)
	}
}

func TestAdmissionSettlementRejectsCreditOverflow(t *testing.T) {
	ctx := context.Background()
	runner := NewDevelopment()
	now := time.Now().UTC()
	reserved, err := runner.ReserveAdmission(ctx, admissionRequest(
		"overflow-reservation", "overflow-run", now,
		api.AdmissionLimits{Window: time.Hour, MaxCredits: math.MaxInt64}, 1,
	))
	if err != nil || !reserved.Allowed {
		t.Fatalf("ReserveAdmission() decision=%#v error=%v", reserved, err)
	}
	active, err := runner.TransitionAdmission(ctx, api.AdmissionTransition{
		ReservationID: "overflow-reservation", ExpectedVersion: reserved.Reservation.Version,
		To: api.AdmissionActive, At: now.Add(time.Minute),
	})
	if err != nil || !active.Allowed {
		t.Fatalf("activate admission decision=%#v error=%v", active, err)
	}
	for _, record := range []api.UsageRecord{
		{ID: "overflow-usage-1", RunID: "overflow-run", Kind: api.UsageKindModelCall, PricingState: api.UsagePricingStatePriced, Credits: math.MaxInt64},
		{ID: "overflow-usage-2", RunID: "overflow-run", Kind: api.UsageKindToolCall, PricingState: api.UsagePricingStatePriced, Credits: 1},
	} {
		if err := runner.AppendUsage(ctx, record); err != nil {
			t.Fatalf("AppendUsage(%s) error = %v", record.ID, err)
		}
	}
	_, err = runner.TransitionAdmission(ctx, api.AdmissionTransition{
		ReservationID: "overflow-reservation", ExpectedVersion: active.Reservation.Version,
		To: api.AdmissionSettled, At: now.Add(2 * time.Minute),
	})
	if !errors.Is(err, api.ErrInvalidTransition) {
		t.Fatalf("settlement error = %v, want ErrInvalidTransition", err)
	}
	current, loadErr := runner.LoadAdmissionReservation(ctx, "overflow-reservation")
	if loadErr != nil {
		t.Fatalf("LoadAdmissionReservation() error = %v", loadErr)
	}
	if current.State != api.AdmissionActive {
		t.Fatalf("admission state = %s, want active after overflow", current.State)
	}
}

func TestAdmissionTransitionRejectsStaleVersion(t *testing.T) {
	ctx := context.Background()
	runner := NewDevelopment()
	now := time.Now().UTC()
	decision, err := runner.ReserveAdmission(ctx, admissionRequest("reservation", "run", now, api.AdmissionLimits{MaxConcurrentRuns: 1}, 0))
	if err != nil || !decision.Allowed {
		t.Fatalf("reserve decision=%#v error=%v", decision, err)
	}
	stale, err := runner.TransitionAdmission(ctx, api.AdmissionTransition{
		ReservationID: "reservation", ExpectedVersion: 0, To: api.AdmissionActive, At: now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("TransitionAdmission() error = %v", err)
	}
	if stale.Allowed || stale.Reason != api.AdmissionDeniedVersionConflict {
		t.Fatalf("stale decision = %#v", stale)
	}
}

func TestAdmissionRequiresAdvertisedStoreCapability(t *testing.T) {
	backing := NewDevelopment()
	runner := NewDevelopment(api.Config{StoreProvider: unreportedStoreProvider{StoreProvider: backing.StoreProvider()}})
	now := time.Now().UTC()
	_, err := runner.ReserveAdmission(context.Background(), admissionRequest("reservation", "run", now, api.AdmissionLimits{}, 0))
	if !errors.Is(err, api.ErrInvalidConfiguration) {
		t.Fatalf("ReserveAdmission() error = %v, want ErrInvalidConfiguration", err)
	}
}

func admissionRequest(id, runID string, now time.Time, limits api.AdmissionLimits, credits int64) api.AdmissionRequest {
	return api.AdmissionRequest{
		ReservationID:   id,
		AgentID:         "agent-1",
		RunID:           runID,
		Limits:          limits,
		ReservedCredits: credits,
		RequestedAt:     now,
		ExpiresAt:       now.Add(30 * time.Minute),
	}
}

func finishAdmissionAgentTask(
	ctx context.Context,
	t *testing.T,
	runner *Runner,
	runID string,
	status api.TaskStatus,
) {
	t.Helper()
	_, root, err := runner.StartRun(ctx, api.StartRunCommand{
		RunID: runID, RootTaskID: runID + "-root", Request: "admission outcome",
	})
	if err != nil {
		t.Fatalf("StartRun(%s) error = %v", runID, err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID: runID, TaskID: runID + "-agent", ParentTaskID: root.ID,
		Type: api.TaskTypeWorker, AssignedAgentID: "agent-1", OwnerAgentID: "agent-1",
	})
	if err != nil {
		t.Fatalf("CreateTask(%s) error = %v", runID, err)
	}
	for _, target := range []api.TaskStatus{api.TaskStatusDispatched, api.TaskStatusRunning, status} {
		if err := runner.TransitionTask(ctx, api.TransitionTaskCommand{
			RunID: runID, TaskID: task.ID, To: target,
		}); err != nil {
			t.Fatalf("TransitionTask(%s, %s) error = %v", runID, target, err)
		}
	}
}

type unreportedStoreProvider struct {
	api.StoreProvider
}
