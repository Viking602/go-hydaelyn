package venat

import (
	"context"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/adapter"
)

// PreviewAdmission evaluates aggregate capacity without reserving it.
func (r *Runner) PreviewAdmission(ctx context.Context, request api.AdmissionRequest) (api.AdmissionDecision, error) {
	decision, err := r.rt.PreviewAdmission(ctx, adapter.AdmissionRequestToModel(request))
	return adapter.AdmissionDecisionFromModel(decision), adapter.ErrorToAPI(err)
}

// ReserveAdmission atomically evaluates aggregate limits and records capacity.
func (r *Runner) ReserveAdmission(ctx context.Context, request api.AdmissionRequest) (api.AdmissionDecision, error) {
	decision, err := r.rt.ReserveAdmission(ctx, adapter.AdmissionRequestToModel(request))
	return adapter.AdmissionDecisionFromModel(decision), adapter.ErrorToAPI(err)
}

// TransitionAdmission applies one expected-version reservation transition.
func (r *Runner) TransitionAdmission(ctx context.Context, transition api.AdmissionTransition) (api.AdmissionDecision, error) {
	decision, err := r.rt.TransitionAdmission(ctx, adapter.AdmissionTransitionToModel(transition))
	return adapter.AdmissionDecisionFromModel(decision), adapter.ErrorToAPI(err)
}

// LoadAdmissionReservation loads one durable aggregate-capacity reservation.
func (r *Runner) LoadAdmissionReservation(ctx context.Context, id string) (api.AdmissionReservation, error) {
	reservation, err := r.rt.LoadAdmissionReservation(ctx, id)
	return adapter.AdmissionReservationFromModel(reservation), adapter.ErrorToAPI(err)
}

// ListAdmissionReservations lists durable reservations matching selector.
func (r *Runner) ListAdmissionReservations(ctx context.Context, selector api.AdmissionReservationSelector) ([]api.AdmissionReservation, error) {
	reservations, err := r.rt.ListAdmissionReservations(ctx, adapter.AdmissionReservationSelectorToModel(selector))
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	out := make([]api.AdmissionReservation, 0, len(reservations))
	for _, reservation := range reservations {
		out = append(out, adapter.AdmissionReservationFromModel(reservation))
	}
	return out, nil
}
