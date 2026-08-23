package core

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/ports"
)

// PreviewAdmission evaluates aggregate capacity without reserving it.
func (r *Runtime) PreviewAdmission(ctx context.Context, request api.AdmissionRequest) (api.AdmissionDecision, error) {
	request, err := admissionRequestAtTrustedTime(request)
	if err != nil {
		return api.AdmissionDecision{}, err
	}
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return api.AdmissionDecision{}, err
	}
	defer func() { _ = done() }()
	store, err := r.admissionStore(ctx, uow)
	if err != nil {
		return api.AdmissionDecision{}, err
	}
	return store.PreviewAdmission(ctx, request)
}

// ReserveAdmission atomically evaluates aggregate limits and records capacity.
func (r *Runtime) ReserveAdmission(ctx context.Context, request api.AdmissionRequest) (decision api.AdmissionDecision, err error) {
	request, err = admissionRequestAtTrustedTime(request)
	if err != nil {
		return api.AdmissionDecision{}, err
	}
	uow, err := r.beginWriteUoW(ctx)
	if err != nil {
		return api.AdmissionDecision{}, err
	}
	committed := false
	defer rollbackIfNotCommitted(ctx, uow, &committed, &err)
	store, err := r.admissionStore(ctx, uow)
	if err != nil {
		return api.AdmissionDecision{}, err
	}
	decision, err = store.ReserveAdmission(ctx, request)
	if err != nil {
		return decision, err
	}
	if err = uow.Commit(ctx); err != nil {
		return api.AdmissionDecision{}, err
	}
	committed = true
	return decision, nil
}

// TransitionAdmission applies one expected-version reservation transition.
func (r *Runtime) TransitionAdmission(ctx context.Context, transition api.AdmissionTransition) (decision api.AdmissionDecision, err error) {
	transition, err = admissionTransitionAtTrustedTime(transition)
	if err != nil {
		return api.AdmissionDecision{}, err
	}
	uow, err := r.beginWriteUoW(ctx)
	if err != nil {
		return api.AdmissionDecision{}, err
	}
	committed := false
	defer rollbackIfNotCommitted(ctx, uow, &committed, &err)
	store, err := r.admissionStore(ctx, uow)
	if err != nil {
		return api.AdmissionDecision{}, err
	}
	if transition.To == api.AdmissionSettled {
		reservation, err := store.LoadAdmissionReservation(ctx, transition.ReservationID)
		if err != nil {
			return api.AdmissionDecision{}, err
		}
		records, err := uow.UsageRecords().QueryUsage(ctx, api.UsageSelector{RunID: reservation.RunID})
		if err != nil {
			return api.AdmissionDecision{}, err
		}
		credits, err := admissionConsumedCredits(records)
		if err != nil {
			return api.AdmissionDecision{}, err
		}
		transition.ConsumedCredits = credits
		failed, err := admissionRunFailed(ctx, uow, reservation)
		if err != nil {
			return api.AdmissionDecision{}, err
		}
		transition.Failed = failed
	}
	decision, err = store.TransitionAdmission(ctx, transition)
	if err != nil {
		return api.AdmissionDecision{}, err
	}
	if err := uow.Commit(ctx); err != nil {
		return api.AdmissionDecision{}, err
	}
	committed = true
	return decision, nil
}

func admissionRequestAtTrustedTime(request api.AdmissionRequest) (api.AdmissionRequest, error) {
	if request.RequestedAt.IsZero() || !request.ExpiresAt.After(request.RequestedAt) {
		return api.AdmissionRequest{}, fmt.Errorf("admission timestamps are invalid: %w", api.ErrInvalidCommand)
	}
	lifetime := request.ExpiresAt.Sub(request.RequestedAt)
	request.RequestedAt = time.Now().UTC()
	request.ExpiresAt = request.RequestedAt.Add(lifetime)
	return request, nil
}

func admissionTransitionAtTrustedTime(transition api.AdmissionTransition) (api.AdmissionTransition, error) {
	if transition.At.IsZero() {
		return api.AdmissionTransition{}, fmt.Errorf("admission transition timestamp is invalid: %w", api.ErrInvalidTransition)
	}
	var lifetime time.Duration
	if !transition.ExpiresAt.IsZero() {
		if !transition.ExpiresAt.After(transition.At) {
			return api.AdmissionTransition{}, fmt.Errorf("admission transition expiry is invalid: %w", api.ErrInvalidTransition)
		}
		lifetime = transition.ExpiresAt.Sub(transition.At)
	}
	transition.At = time.Now().UTC()
	if lifetime > 0 {
		transition.ExpiresAt = transition.At.Add(lifetime)
	}
	return transition, nil
}

func admissionConsumedCredits(records []api.UsageRecord) (int64, error) {
	var credits int64
	for _, record := range records {
		if record.PricingState != api.UsagePricingStatePriced {
			return 0, api.ErrUsageUnpriced
		}
		if record.Credits < 0 || credits > math.MaxInt64-record.Credits {
			return 0, fmt.Errorf("admission consumed credits overflow: %w", api.ErrInvalidTransition)
		}
		credits += record.Credits
	}
	return credits, nil
}

func admissionRunFailed(ctx context.Context, uow ports.UnitOfWork, reservation api.AdmissionReservation) (bool, error) {
	run, err := uow.Runs().LoadRun(ctx, reservation.RunID)
	if err != nil {
		return false, fmt.Errorf("admission settlement requires durable run %q: %w", reservation.RunID, api.ErrInvalidTransition)
	}
	tasks, err := uow.Tasks().ListTasks(ctx, run.ID)
	if err != nil {
		return false, err
	}
	matched, failed := false, false
	for _, task := range tasks {
		if task.ID == run.RootTaskID ||
			(task.AssignedAgentID != reservation.AgentID && task.OwnerAgentID != reservation.AgentID) {
			continue
		}
		matched = true
		switch task.Status {
		case api.TaskStatusFailed:
			failed = true
		case api.TaskStatusCompleted, api.TaskStatusCancelled:
		default:
			return false, fmt.Errorf(
				"admission settlement requires terminal agent task %q, got %q: %w",
				task.ID,
				task.Status,
				api.ErrInvalidTransition,
			)
		}
	}
	if matched {
		return failed, nil
	}
	switch run.Status {
	case api.RunStatusFailed:
		return true, nil
	case api.RunStatusCompleted, api.RunStatusCancelled:
		return false, nil
	default:
		return false, fmt.Errorf(
			"admission settlement requires terminal state for agent %q in run %q: %w",
			reservation.AgentID,
			reservation.RunID,
			api.ErrInvalidTransition,
		)
	}
}

func (r *Runtime) LoadAdmissionReservation(ctx context.Context, id string) (api.AdmissionReservation, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return api.AdmissionReservation{}, err
	}
	defer func() { _ = done() }()
	store, err := r.admissionStore(ctx, uow)
	if err != nil {
		return api.AdmissionReservation{}, err
	}
	return store.LoadAdmissionReservation(ctx, id)
}

func (r *Runtime) ListAdmissionReservations(ctx context.Context, selector api.AdmissionReservationSelector) ([]api.AdmissionReservation, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = done() }()
	store, err := r.admissionStore(ctx, uow)
	if err != nil {
		return nil, err
	}
	return store.ListAdmissionReservations(ctx, selector)
}

func (r *Runtime) admissionStore(ctx context.Context, uow ports.UnitOfWork) (ports.AdmissionReservationStore, error) {
	capabilities, err := r.StoreCapabilities(ctx)
	if err != nil {
		return nil, err
	}
	if !capabilities.SupportsAdmissionReservations {
		return nil, fmt.Errorf("admission reservation storage is not supported: %w", api.ErrInvalidConfiguration)
	}
	extension, ok := uow.(ports.AdmissionReservationUnitOfWork)
	if !ok || extension.AdmissionReservations() == nil {
		return nil, fmt.Errorf("provider advertises admission reservations without exposing the store: %w", api.ErrInvalidConfiguration)
	}
	return extension.AdmissionReservations(), nil
}
