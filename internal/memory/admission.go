package memory

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/Viking602/venat/internal/core/model"
)

func (s *admissionReservationStore) ensureOpen() error {
	return (*UnitOfWork)(s).ensureOpen()
}

type admissionReservationStore UnitOfWork

func (s *admissionReservationStore) PreviewAdmission(_ context.Context, request model.AdmissionRequest) (model.AdmissionDecision, error) {
	if err := s.ensureOpen(); err != nil {
		return model.AdmissionDecision{}, err
	}
	if err := validateAdmissionRequest(request); err != nil {
		return model.AdmissionDecision{}, err
	}
	return evaluateAdmission(s.staged, request, ""), nil
}

func (s *admissionReservationStore) ReserveAdmission(_ context.Context, request model.AdmissionRequest) (model.AdmissionDecision, error) {
	if err := s.ensureOpen(); err != nil {
		return model.AdmissionDecision{}, err
	}
	if err := validateAdmissionRequest(request); err != nil {
		return model.AdmissionDecision{}, err
	}
	if existing, ok := s.staged.AdmissionReservations[request.ReservationID]; ok {
		if admissionRequestMatches(existing, request) {
			return model.AdmissionDecision{Allowed: true, Reservation: existing}, nil
		}
		return model.AdmissionDecision{}, fmt.Errorf("admission reservation %q: %w", request.ReservationID, model.ErrIdempotencyConflict)
	}
	for _, existing := range s.staged.AdmissionReservations {
		if existing.AgentID == request.AgentID && existing.RunID == request.RunID {
			return model.AdmissionDecision{}, fmt.Errorf("admission run %q already has reservation %q: %w", request.RunID, existing.ID, model.ErrIdempotencyConflict)
		}
	}
	decision := evaluateAdmission(s.staged, request, "")
	if !decision.Allowed {
		return decision, nil
	}
	reservation := model.AdmissionReservation{
		ID: request.ReservationID, AgentID: request.AgentID, AgentVersion: request.AgentVersion, RunID: request.RunID,
		State: model.AdmissionReserved, Limits: request.Limits, ReservedCredits: request.ReservedCredits,
		Version: 1, CreatedAt: request.RequestedAt, UpdatedAt: request.RequestedAt, ExpiresAt: request.ExpiresAt,
	}
	s.staged.AdmissionReservations[reservation.ID] = reservation
	decision.Reservation = reservation
	return decision, nil
}

func (s *admissionReservationStore) TransitionAdmission(_ context.Context, transition model.AdmissionTransition) (model.AdmissionDecision, error) {
	if err := s.ensureOpen(); err != nil {
		return model.AdmissionDecision{}, err
	}
	reservation, ok := s.staged.AdmissionReservations[transition.ReservationID]
	if !ok {
		return model.AdmissionDecision{}, model.ErrNotFound
	}
	if reservation.Version != transition.ExpectedVersion {
		return model.AdmissionDecision{
			Allowed: false,
			Reason:  model.AdmissionDeniedVersionConflict,
			Usage:   admissionUsage(s.staged, reservation.AgentID, reservation.Limits, transition.At, ""),
		}, nil
	}
	if err := validateAdmissionTransition(reservation, transition); err != nil {
		return model.AdmissionDecision{}, err
	}
	usage := admissionUsage(s.staged, reservation.AgentID, reservation.Limits, transition.At, reservation.ID)
	if reservation.State == model.AdmissionSuspended && transition.To == model.AdmissionActive &&
		reservation.Limits.MaxConcurrentRuns > 0 && usage.ConcurrentRuns+1 > reservation.Limits.MaxConcurrentRuns {
		return model.AdmissionDecision{Allowed: false, Reason: model.AdmissionDeniedConcurrency, Usage: usage}, nil
	}

	reservation.State = transition.To
	reservation.Version++
	reservation.UpdatedAt = transition.At
	if !transition.ExpiresAt.IsZero() {
		reservation.ExpiresAt = transition.ExpiresAt
	}
	switch transition.To {
	case model.AdmissionActive:
		if reservation.ActivatedAt.IsZero() {
			reservation.ActivatedAt = transition.At
		}
	case model.AdmissionSettled:
		reservation.ConsumedCredits = transition.ConsumedCredits
		reservation.Failed = transition.Failed
		reservation.SettledAt = transition.At
	}
	s.staged.AdmissionReservations[reservation.ID] = reservation
	return model.AdmissionDecision{Allowed: true, Usage: usage, Reservation: reservation}, nil
}

func (s *admissionReservationStore) LoadAdmissionReservation(_ context.Context, id string) (model.AdmissionReservation, error) {
	if err := s.ensureOpen(); err != nil {
		return model.AdmissionReservation{}, err
	}
	reservation, ok := s.staged.AdmissionReservations[id]
	if !ok {
		return model.AdmissionReservation{}, model.ErrNotFound
	}
	return reservation, nil
}

func (s *admissionReservationStore) ListAdmissionReservations(_ context.Context, selector model.AdmissionReservationSelector) ([]model.AdmissionReservation, error) {
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}
	out := make([]model.AdmissionReservation, 0)
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

func validateAdmissionRequest(request model.AdmissionRequest) error {
	if request.ReservationID == "" || request.AgentID == "" || request.RunID == "" {
		return fmt.Errorf("admission reservation, agent, and run ids are required: %w", model.ErrInvalidCommand)
	}
	if request.RequestedAt.IsZero() || !request.ExpiresAt.After(request.RequestedAt) {
		return fmt.Errorf("admission timestamps are invalid: %w", model.ErrInvalidCommand)
	}
	if request.ReservedCredits < 0 || request.Limits.MaxConcurrentRuns < 0 || request.Limits.MaxRunsPerWindow < 0 ||
		request.Limits.MaxCredits < 0 || request.Limits.PauseOnExcessFailures < 0 {
		return fmt.Errorf("admission limits cannot be negative: %w", model.ErrInvalidCommand)
	}
	usesWindow := request.Limits.MaxRunsPerWindow > 0 || request.Limits.MaxCredits > 0 || request.Limits.PauseOnExcessFailures > 0
	if usesWindow && request.Limits.Window <= 0 {
		return fmt.Errorf("admission window is required by aggregate limits: %w", model.ErrInvalidCommand)
	}
	return nil
}

func validateAdmissionTransition(reservation model.AdmissionReservation, transition model.AdmissionTransition) error {
	if transition.At.IsZero() || transition.At.Before(reservation.UpdatedAt) {
		return fmt.Errorf("admission transition timestamp is invalid: %w", model.ErrInvalidTransition)
	}
	if transition.ConsumedCredits < 0 {
		return fmt.Errorf("admission consumed credits cannot be negative: %w", model.ErrInvalidTransition)
	}
	if !transition.ExpiresAt.IsZero() && !transition.ExpiresAt.After(transition.At) {
		return fmt.Errorf("admission transition expiry is invalid: %w", model.ErrInvalidTransition)
	}
	if !validAdmissionTransition(reservation.State, transition.To) {
		return fmt.Errorf("admission transition %q to %q: %w", reservation.State, transition.To, model.ErrInvalidTransition)
	}
	if !reservation.ExpiresAt.After(transition.At) && transition.To != model.AdmissionExpired && transition.To != model.AdmissionSettled {
		return fmt.Errorf("admission reservation has expired: %w", model.ErrInvalidTransition)
	}
	if transition.To != model.AdmissionSettled && (transition.ConsumedCredits != 0 || transition.Failed) {
		return fmt.Errorf("admission outcome is only valid when settling: %w", model.ErrInvalidTransition)
	}
	return nil
}

func validAdmissionTransition(from, to model.AdmissionState) bool {
	switch from {
	case model.AdmissionReserved:
		return to == model.AdmissionActive || to == model.AdmissionReleased || to == model.AdmissionExpired
	case model.AdmissionActive:
		return to == model.AdmissionSuspended || to == model.AdmissionSettled || to == model.AdmissionReleased || to == model.AdmissionExpired
	case model.AdmissionSuspended:
		return to == model.AdmissionActive || to == model.AdmissionSettled || to == model.AdmissionReleased || to == model.AdmissionExpired
	default:
		return false
	}
}

func evaluateAdmission(state *State, request model.AdmissionRequest, excludeID string) model.AdmissionDecision {
	usage := admissionUsage(state, request.AgentID, request.Limits, request.RequestedAt, excludeID)
	switch {
	case request.Limits.MaxConcurrentRuns > 0 && usage.ConcurrentRuns+1 > request.Limits.MaxConcurrentRuns:
		return model.AdmissionDecision{Reason: model.AdmissionDeniedConcurrency, Usage: usage}
	case request.Limits.MaxRunsPerWindow > 0 && usage.RunsInWindow+1 > request.Limits.MaxRunsPerWindow:
		return model.AdmissionDecision{Reason: model.AdmissionDeniedRunWindow, Usage: usage}
	case request.Limits.MaxCredits > 0 && usage.CommittedCredits+usage.ReservedCredits+request.ReservedCredits > request.Limits.MaxCredits:
		return model.AdmissionDecision{Reason: model.AdmissionDeniedCredits, Usage: usage}
	case request.Limits.PauseOnExcessFailures > 0 && usage.TrailingFailures >= request.Limits.PauseOnExcessFailures:
		return model.AdmissionDecision{Reason: model.AdmissionDeniedFailureBreaker, Usage: usage}
	default:
		return model.AdmissionDecision{Allowed: true, Usage: usage}
	}
}

func admissionUsage(state *State, agentID string, limits model.AdmissionLimits, now time.Time, excludeID string) model.AdmissionUsage {
	usage := model.AdmissionUsage{}
	windowStart := now.Add(-limits.Window)
	settled := make([]model.AdmissionReservation, 0)
	for _, reservation := range state.AdmissionReservations {
		if reservation.ID == excludeID || reservation.AgentID != agentID || reservation.State == model.AdmissionReleased || reservation.State == model.AdmissionExpired {
			continue
		}
		expired := !reservation.ExpiresAt.After(now) && reservation.State != model.AdmissionSettled
		if !expired && (reservation.State == model.AdmissionReserved || reservation.State == model.AdmissionActive) {
			usage.ConcurrentRuns++
		}
		inWindow := limits.Window > 0 && !reservation.CreatedAt.Before(windowStart)
		if !inWindow {
			continue
		}
		usage.RunsInWindow++
		switch reservation.State {
		case model.AdmissionReserved, model.AdmissionActive, model.AdmissionSuspended:
			if !expired {
				usage.ReservedCredits += reservation.ReservedCredits
			}
		case model.AdmissionSettled:
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

func admissionRequestMatches(reservation model.AdmissionReservation, request model.AdmissionRequest) bool {
	return reservation.ID == request.ReservationID && reservation.AgentID == request.AgentID &&
		reservation.AgentVersion == request.AgentVersion && reservation.RunID == request.RunID &&
		reservation.Limits == request.Limits && reservation.ReservedCredits == request.ReservedCredits
}

func matchesAdmissionSelector(reservation model.AdmissionReservation, selector model.AdmissionReservationSelector) bool {
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

func containsAdmissionState(values []model.AdmissionState, target model.AdmissionState) bool {
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
