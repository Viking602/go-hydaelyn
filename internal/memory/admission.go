package memory

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/Viking602/venat/api"
)

func (s *admissionReservationStore) ensureOpen() error {
	return (*UnitOfWork)(s).ensureOpen()
}

type admissionReservationStore UnitOfWork

func (s *admissionReservationStore) PreviewAdmission(_ context.Context, request api.AdmissionRequest) (api.AdmissionDecision, error) {
	if err := s.ensureOpen(); err != nil {
		return api.AdmissionDecision{}, err
	}
	if err := validateAdmissionRequest(request); err != nil {
		return api.AdmissionDecision{}, err
	}
	return evaluateAdmission(s.staged, request, ""), nil
}

func (s *admissionReservationStore) ReserveAdmission(_ context.Context, request api.AdmissionRequest) (api.AdmissionDecision, error) {
	if err := s.ensureOpen(); err != nil {
		return api.AdmissionDecision{}, err
	}
	if err := validateAdmissionRequest(request); err != nil {
		return api.AdmissionDecision{}, err
	}
	if existing, ok := s.staged.AdmissionReservations[request.ReservationID]; ok {
		if admissionRequestMatches(existing, request) {
			return api.AdmissionDecision{Allowed: true, Reservation: existing}, nil
		}
		return api.AdmissionDecision{}, fmt.Errorf("admission reservation %q: %w", request.ReservationID, api.ErrIdempotencyConflict)
	}
	for _, existing := range s.staged.AdmissionReservations {
		if existing.AgentID == request.AgentID && existing.RunID == request.RunID {
			return api.AdmissionDecision{}, fmt.Errorf("admission run %q already has reservation %q: %w", request.RunID, existing.ID, api.ErrIdempotencyConflict)
		}
	}
	decision := evaluateAdmission(s.staged, request, "")
	if !decision.Allowed {
		return decision, nil
	}
	reservation := api.AdmissionReservation{
		ID: request.ReservationID, AgentID: request.AgentID, AgentVersion: request.AgentVersion, RunID: request.RunID,
		State: api.AdmissionReserved, Limits: request.Limits, ReservedCredits: request.ReservedCredits,
		Version: 1, CreatedAt: request.RequestedAt, UpdatedAt: request.RequestedAt, ExpiresAt: request.ExpiresAt,
	}
	s.staged.AdmissionReservations[reservation.ID] = reservation
	decision.Reservation = reservation
	return decision, nil
}

func (s *admissionReservationStore) TransitionAdmission(_ context.Context, transition api.AdmissionTransition) (api.AdmissionDecision, error) {
	if err := s.ensureOpen(); err != nil {
		return api.AdmissionDecision{}, err
	}
	reservation, ok := s.staged.AdmissionReservations[transition.ReservationID]
	if !ok {
		return api.AdmissionDecision{}, api.ErrNotFound
	}
	if reservation.Version != transition.ExpectedVersion {
		return api.AdmissionDecision{
			Allowed: false,
			Reason:  api.AdmissionDeniedVersionConflict,
			Usage:   admissionUsage(s.staged, reservation.AgentID, reservation.Limits, transition.At, ""),
		}, nil
	}
	if err := validateAdmissionTransition(reservation, transition); err != nil {
		return api.AdmissionDecision{}, err
	}
	usage := admissionUsage(s.staged, reservation.AgentID, reservation.Limits, transition.At, reservation.ID)
	if reservation.State == api.AdmissionSuspended && transition.To == api.AdmissionActive &&
		reservation.Limits.MaxConcurrentRuns > 0 && usage.ConcurrentRuns+1 > reservation.Limits.MaxConcurrentRuns {
		return api.AdmissionDecision{Allowed: false, Reason: api.AdmissionDeniedConcurrency, Usage: usage}, nil
	}

	reservation.State = transition.To
	reservation.Version++
	reservation.UpdatedAt = transition.At
	if !transition.ExpiresAt.IsZero() {
		reservation.ExpiresAt = transition.ExpiresAt
	}
	switch transition.To {
	case api.AdmissionActive:
		if reservation.ActivatedAt.IsZero() {
			reservation.ActivatedAt = transition.At
		}
	case api.AdmissionSettled:
		reservation.ConsumedCredits = transition.ConsumedCredits
		reservation.Failed = transition.Failed
		reservation.SettledAt = transition.At
	}
	s.staged.AdmissionReservations[reservation.ID] = reservation
	return api.AdmissionDecision{Allowed: true, Usage: usage, Reservation: reservation}, nil
}

func (s *admissionReservationStore) LoadAdmissionReservation(_ context.Context, id string) (api.AdmissionReservation, error) {
	if err := s.ensureOpen(); err != nil {
		return api.AdmissionReservation{}, err
	}
	reservation, ok := s.staged.AdmissionReservations[id]
	if !ok {
		return api.AdmissionReservation{}, api.ErrNotFound
	}
	return reservation, nil
}

func (s *admissionReservationStore) ListAdmissionReservations(_ context.Context, selector api.AdmissionReservationSelector) ([]api.AdmissionReservation, error) {
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}
	out := make([]api.AdmissionReservation, 0)
	for _, reservation := range s.staged.AdmissionReservations {
		if !matchesAdmissionSelector(reservation, selector) {
			continue
		}
		out = append(out, reservation)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	if selector.Limit > 0 && len(out) > selector.Limit {
		out = out[:selector.Limit]
	}
	return out, nil
}

func validateAdmissionRequest(request api.AdmissionRequest) error {
	if request.ReservationID == "" || request.AgentID == "" || request.RunID == "" {
		return fmt.Errorf("admission reservation, agent, and run ids are required: %w", api.ErrInvalidCommand)
	}
	if request.RequestedAt.IsZero() || !request.ExpiresAt.After(request.RequestedAt) {
		return fmt.Errorf("admission timestamps are invalid: %w", api.ErrInvalidCommand)
	}
	if request.ReservedCredits < 0 || request.Limits.MaxConcurrentRuns < 0 || request.Limits.MaxRunsPerWindow < 0 ||
		request.Limits.MaxCredits < 0 || request.Limits.PauseOnExcessFailures < 0 {
		return fmt.Errorf("admission limits cannot be negative: %w", api.ErrInvalidCommand)
	}
	usesWindow := request.Limits.MaxRunsPerWindow > 0 || request.Limits.MaxCredits > 0 || request.Limits.PauseOnExcessFailures > 0
	if usesWindow && request.Limits.Window <= 0 {
		return fmt.Errorf("admission window is required by aggregate limits: %w", api.ErrInvalidCommand)
	}
	return nil
}

func validateAdmissionTransition(reservation api.AdmissionReservation, transition api.AdmissionTransition) error {
	if transition.At.IsZero() || transition.At.Before(reservation.UpdatedAt) {
		return fmt.Errorf("admission transition timestamp is invalid: %w", api.ErrInvalidTransition)
	}
	if transition.ConsumedCredits < 0 {
		return fmt.Errorf("admission consumed credits cannot be negative: %w", api.ErrInvalidTransition)
	}
	if !transition.ExpiresAt.IsZero() && !transition.ExpiresAt.After(transition.At) {
		return fmt.Errorf("admission transition expiry is invalid: %w", api.ErrInvalidTransition)
	}
	if !validAdmissionTransition(reservation.State, transition.To) {
		return fmt.Errorf("admission transition %q to %q: %w", reservation.State, transition.To, api.ErrInvalidTransition)
	}
	if !reservation.ExpiresAt.After(transition.At) && transition.To != api.AdmissionExpired && transition.To != api.AdmissionSettled {
		return fmt.Errorf("admission reservation has expired: %w", api.ErrInvalidTransition)
	}
	if transition.To != api.AdmissionSettled && (transition.ConsumedCredits != 0 || transition.Failed) {
		return fmt.Errorf("admission outcome is only valid when settling: %w", api.ErrInvalidTransition)
	}
	return nil
}

func validAdmissionTransition(from, to api.AdmissionState) bool {
	switch from {
	case api.AdmissionReserved:
		return to == api.AdmissionActive || to == api.AdmissionReleased || to == api.AdmissionExpired
	case api.AdmissionActive:
		return to == api.AdmissionSuspended || to == api.AdmissionSettled || to == api.AdmissionReleased || to == api.AdmissionExpired
	case api.AdmissionSuspended:
		return to == api.AdmissionActive || to == api.AdmissionSettled || to == api.AdmissionReleased || to == api.AdmissionExpired
	default:
		return false
	}
}

func evaluateAdmission(state *State, request api.AdmissionRequest, excludeID string) api.AdmissionDecision {
	usage := admissionUsage(state, request.AgentID, request.Limits, request.RequestedAt, excludeID)
	switch {
	case request.Limits.MaxConcurrentRuns > 0 && usage.ConcurrentRuns+1 > request.Limits.MaxConcurrentRuns:
		return api.AdmissionDecision{Reason: api.AdmissionDeniedConcurrency, Usage: usage}
	case request.Limits.MaxRunsPerWindow > 0 && usage.RunsInWindow+1 > request.Limits.MaxRunsPerWindow:
		return api.AdmissionDecision{Reason: api.AdmissionDeniedRunWindow, Usage: usage}
	case request.Limits.MaxCredits > 0 && usage.CommittedCredits+usage.ReservedCredits+request.ReservedCredits > request.Limits.MaxCredits:
		return api.AdmissionDecision{Reason: api.AdmissionDeniedCredits, Usage: usage}
	case request.Limits.PauseOnExcessFailures > 0 && usage.TrailingFailures >= request.Limits.PauseOnExcessFailures:
		return api.AdmissionDecision{Reason: api.AdmissionDeniedFailureBreaker, Usage: usage}
	default:
		return api.AdmissionDecision{Allowed: true, Usage: usage}
	}
}

func admissionUsage(state *State, agentID string, limits api.AdmissionLimits, now time.Time, excludeID string) api.AdmissionUsage {
	usage := api.AdmissionUsage{}
	windowStart := now.Add(-limits.Window)
	settled := make([]api.AdmissionReservation, 0)
	for _, reservation := range state.AdmissionReservations {
		if reservation.ID == excludeID || reservation.AgentID != agentID || reservation.State == api.AdmissionReleased || reservation.State == api.AdmissionExpired {
			continue
		}
		expired := !reservation.ExpiresAt.After(now) && reservation.State != api.AdmissionSettled
		if !expired && (reservation.State == api.AdmissionReserved || reservation.State == api.AdmissionActive) {
			usage.ConcurrentRuns++
		}
		inWindow := limits.Window > 0 && !reservation.CreatedAt.Before(windowStart)
		if !inWindow {
			continue
		}
		usage.RunsInWindow++
		switch reservation.State {
		case api.AdmissionReserved, api.AdmissionActive, api.AdmissionSuspended:
			if !expired {
				usage.ReservedCredits += reservation.ReservedCredits
			}
		case api.AdmissionSettled:
			usage.CommittedCredits += reservation.ConsumedCredits
			settled = append(settled, reservation)
		}
	}
	sort.Slice(settled, func(i, j int) bool {
		if settled[i].SettledAt.Equal(settled[j].SettledAt) {
			return settled[i].ID > settled[j].ID
		}
		return settled[i].SettledAt.After(settled[j].SettledAt)
	})
	for _, reservation := range settled {
		if !reservation.Failed {
			break
		}
		usage.TrailingFailures++
	}
	return usage
}

func admissionRequestMatches(reservation api.AdmissionReservation, request api.AdmissionRequest) bool {
	return reservation.ID == request.ReservationID && reservation.AgentID == request.AgentID &&
		reservation.AgentVersion == request.AgentVersion && reservation.RunID == request.RunID &&
		reservation.Limits == request.Limits && reservation.ReservedCredits == request.ReservedCredits
}

func matchesAdmissionSelector(reservation api.AdmissionReservation, selector api.AdmissionReservationSelector) bool {
	return containsAdmissionString(selector.AgentIDs, reservation.AgentID) &&
		containsAdmissionString(selector.RunIDs, reservation.RunID) &&
		containsAdmissionState(selector.States, reservation.State) &&
		(selector.Since.IsZero() || !reservation.CreatedAt.Before(selector.Since)) &&
		(selector.ExpiresBefore.IsZero() || !reservation.ExpiresAt.After(selector.ExpiresBefore))
}

func containsAdmissionString(values []string, target string) bool {
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

func containsAdmissionState(values []api.AdmissionState, target api.AdmissionState) bool {
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
