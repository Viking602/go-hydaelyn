package venat

import (
	"context"

	"github.com/Viking602/venat/api"
)

// PreviewAdmission evaluates aggregate capacity without reserving it.
func (r *Runner) PreviewAdmission(ctx context.Context, request api.AdmissionRequest) (api.AdmissionDecision, error) {
	decision, err := r.rt.PreviewAdmission(ctx, request)
	return decision, err
}

// ReserveAdmission atomically evaluates aggregate limits and records capacity.
func (r *Runner) ReserveAdmission(ctx context.Context, request api.AdmissionRequest) (api.AdmissionDecision, error) {
	decision, err := r.rt.ReserveAdmission(ctx, request)
	return decision, err
}

// TransitionAdmission applies one expected-version reservation transition.
func (r *Runner) TransitionAdmission(ctx context.Context, transition api.AdmissionTransition) (api.AdmissionDecision, error) {
	decision, err := r.rt.TransitionAdmission(ctx, transition)
	return decision, err
}

// LoadAdmissionReservation loads one durable aggregate-capacity reservation.
func (r *Runner) LoadAdmissionReservation(ctx context.Context, id string) (api.AdmissionReservation, error) {
	reservation, err := r.rt.LoadAdmissionReservation(ctx, id)
	return reservation, err
}

// ListAdmissionReservations lists durable reservations matching selector.
func (r *Runner) ListAdmissionReservations(ctx context.Context, selector api.AdmissionReservationSelector) ([]api.AdmissionReservation, error) {
	reservations, err := r.rt.ListAdmissionReservations(ctx, selector)
	if err != nil {
		return nil, err
	}
	return append([]api.AdmissionReservation(nil), reservations...), nil
}
