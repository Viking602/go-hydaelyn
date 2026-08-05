package adapter

import (
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/model"
)

func AdmissionLimitsToModel(in api.AdmissionLimits) model.AdmissionLimits {
	return model.AdmissionLimits{
		Window:                in.Window,
		MaxConcurrentRuns:     in.MaxConcurrentRuns,
		MaxRunsPerWindow:      in.MaxRunsPerWindow,
		MaxCredits:            in.MaxCredits,
		PauseOnExcessFailures: in.PauseOnExcessFailures,
	}
}

func AdmissionLimitsFromModel(in model.AdmissionLimits) api.AdmissionLimits {
	return api.AdmissionLimits{
		Window:                in.Window,
		MaxConcurrentRuns:     in.MaxConcurrentRuns,
		MaxRunsPerWindow:      in.MaxRunsPerWindow,
		MaxCredits:            in.MaxCredits,
		PauseOnExcessFailures: in.PauseOnExcessFailures,
	}
}

func AdmissionReservationToModel(in api.AdmissionReservation) model.AdmissionReservation {
	return model.AdmissionReservation{
		ID: in.ID, AgentID: in.AgentID, AgentVersion: in.AgentVersion, RunID: in.RunID,
		State: model.AdmissionState(in.State), Limits: AdmissionLimitsToModel(in.Limits),
		ReservedCredits: in.ReservedCredits, ConsumedCredits: in.ConsumedCredits, Failed: in.Failed,
		Version: in.Version, CreatedAt: in.CreatedAt, UpdatedAt: in.UpdatedAt,
		ActivatedAt: in.ActivatedAt, SettledAt: in.SettledAt, ExpiresAt: in.ExpiresAt,
	}
}

func AdmissionReservationFromModel(in model.AdmissionReservation) api.AdmissionReservation {
	return api.AdmissionReservation{
		ID: in.ID, AgentID: in.AgentID, AgentVersion: in.AgentVersion, RunID: in.RunID,
		State: api.AdmissionState(in.State), Limits: AdmissionLimitsFromModel(in.Limits),
		ReservedCredits: in.ReservedCredits, ConsumedCredits: in.ConsumedCredits, Failed: in.Failed,
		Version: in.Version, CreatedAt: in.CreatedAt, UpdatedAt: in.UpdatedAt,
		ActivatedAt: in.ActivatedAt, SettledAt: in.SettledAt, ExpiresAt: in.ExpiresAt,
	}
}

func AdmissionRequestToModel(in api.AdmissionRequest) model.AdmissionRequest {
	return model.AdmissionRequest{
		ReservationID: in.ReservationID, AgentID: in.AgentID, AgentVersion: in.AgentVersion, RunID: in.RunID,
		Limits: AdmissionLimitsToModel(in.Limits), ReservedCredits: in.ReservedCredits,
		RequestedAt: in.RequestedAt, ExpiresAt: in.ExpiresAt,
	}
}

func AdmissionRequestFromModel(in model.AdmissionRequest) api.AdmissionRequest {
	return api.AdmissionRequest{
		ReservationID: in.ReservationID, AgentID: in.AgentID, AgentVersion: in.AgentVersion, RunID: in.RunID,
		Limits: AdmissionLimitsFromModel(in.Limits), ReservedCredits: in.ReservedCredits,
		RequestedAt: in.RequestedAt, ExpiresAt: in.ExpiresAt,
	}
}

func AdmissionUsageToModel(in api.AdmissionUsage) model.AdmissionUsage {
	return model.AdmissionUsage{
		ConcurrentRuns: in.ConcurrentRuns, RunsInWindow: in.RunsInWindow,
		ReservedCredits: in.ReservedCredits, CommittedCredits: in.CommittedCredits,
		TrailingFailures: in.TrailingFailures,
	}
}

func AdmissionUsageFromModel(in model.AdmissionUsage) api.AdmissionUsage {
	return api.AdmissionUsage{
		ConcurrentRuns: in.ConcurrentRuns, RunsInWindow: in.RunsInWindow,
		ReservedCredits: in.ReservedCredits, CommittedCredits: in.CommittedCredits,
		TrailingFailures: in.TrailingFailures,
	}
}

func AdmissionDecisionToModel(in api.AdmissionDecision) model.AdmissionDecision {
	return model.AdmissionDecision{
		Allowed: in.Allowed, Reason: model.AdmissionDenialReason(in.Reason),
		Usage: AdmissionUsageToModel(in.Usage), Reservation: AdmissionReservationToModel(in.Reservation),
	}
}

func AdmissionDecisionFromModel(in model.AdmissionDecision) api.AdmissionDecision {
	return api.AdmissionDecision{
		Allowed: in.Allowed, Reason: api.AdmissionDenialReason(in.Reason),
		Usage: AdmissionUsageFromModel(in.Usage), Reservation: AdmissionReservationFromModel(in.Reservation),
	}
}

func AdmissionTransitionToModel(in api.AdmissionTransition) model.AdmissionTransition {
	return model.AdmissionTransition{
		ReservationID: in.ReservationID, ExpectedVersion: in.ExpectedVersion,
		To: model.AdmissionState(in.To), At: in.At, ExpiresAt: in.ExpiresAt,
		ConsumedCredits: in.ConsumedCredits, Failed: in.Failed,
	}
}

func AdmissionTransitionFromModel(in model.AdmissionTransition) api.AdmissionTransition {
	return api.AdmissionTransition{
		ReservationID: in.ReservationID, ExpectedVersion: in.ExpectedVersion,
		To: api.AdmissionState(in.To), At: in.At, ExpiresAt: in.ExpiresAt,
		ConsumedCredits: in.ConsumedCredits, Failed: in.Failed,
	}
}

func AdmissionReservationSelectorToModel(in api.AdmissionReservationSelector) model.AdmissionReservationSelector {
	states := make([]model.AdmissionState, len(in.States))
	for index, state := range in.States {
		states[index] = model.AdmissionState(state)
	}
	return model.AdmissionReservationSelector{
		AgentIDs: cloneStrings(in.AgentIDs), RunIDs: cloneStrings(in.RunIDs), States: states,
		Since: in.Since, ExpiresBefore: in.ExpiresBefore, Limit: in.Limit,
	}
}

func AdmissionReservationSelectorFromModel(in model.AdmissionReservationSelector) api.AdmissionReservationSelector {
	states := make([]api.AdmissionState, len(in.States))
	for index, state := range in.States {
		states[index] = api.AdmissionState(state)
	}
	return api.AdmissionReservationSelector{
		AgentIDs: cloneStrings(in.AgentIDs), RunIDs: cloneStrings(in.RunIDs), States: states,
		Since: in.Since, ExpiresBefore: in.ExpiresBefore, Limit: in.Limit,
	}
}
