package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Viking602/venat"
	"github.com/Viking602/venat/api"
)

func TestStandardAdmissionControllerMapsGovernanceAndRetriesIdempotently(t *testing.T) {
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	controller := StandardAdmissionController{
		Runner: venat.NewDevelopment(),
		Now:    func() time.Time { return now },
		TTL:    5 * time.Minute,
	}
	request := RunAdmissionRequest{
		AgentID: "agent-1", AgentVersion: "v3", RunID: "run-1",
		Governance: api.GovernancePolicy{
			Budget:            api.Budget{MaxCredits: 30, MaxRuntime: 20 * time.Minute},
			Quota:             api.Quota{Window: time.Hour, MaxRunsPerWindow: 4, MaxCredits: 100},
			MaxConcurrentRuns: 2, PauseOnExcessFailures: 3,
		},
	}
	first, err := controller.Reserve(context.Background(), request)
	if err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	if !first.Allowed {
		t.Fatalf("Reserve() decision = %#v", first)
	}
	reservation := first.Reservation
	if reservation.Limits.MaxConcurrentRuns != 2 || reservation.Limits.MaxRunsPerWindow != 4 || reservation.Limits.MaxCredits != 100 || reservation.Limits.PauseOnExcessFailures != 3 {
		t.Fatalf("reservation limits = %#v", reservation.Limits)
	}
	if reservation.ReservedCredits != 30 || reservation.ExpiresAt.Sub(reservation.CreatedAt) != 20*time.Minute {
		t.Fatalf("reservation credits/lifetime = %#v", reservation)
	}

	now = now.Add(time.Minute)
	retried, err := controller.Reserve(context.Background(), request)
	if err != nil {
		t.Fatalf("retried Reserve() error = %v", err)
	}
	if !retried.Allowed || retried.Reservation.ID != reservation.ID || retried.Reservation.Version != reservation.Version {
		t.Fatalf("retried decision = %#v", retried)
	}
}

func TestStandardAdmissionControllerSkipsStorageWithoutAggregateLimits(t *testing.T) {
	decision, err := (StandardAdmissionController{}).Reserve(context.Background(), RunAdmissionRequest{
		AgentID: "agent-1", RunID: "run-1", Governance: api.GovernancePolicy{Budget: api.Budget{MaxTokens: 10}},
	})
	if err != nil || !decision.Allowed || decision.Reservation.ID != "" {
		t.Fatalf("Reserve() decision=%#v error=%v", decision, err)
	}
}

func TestStandardAdmissionControllerRequiresCreditReservationBound(t *testing.T) {
	_, err := (StandardAdmissionController{Runner: venat.NewDevelopment()}).Reserve(context.Background(), RunAdmissionRequest{
		AgentID: "agent-1", RunID: "run-1",
		Governance: api.GovernancePolicy{Quota: api.Quota{Window: time.Hour, MaxCredits: 10}},
	})
	if !errors.Is(err, api.ErrInvalidConfiguration) {
		t.Fatalf("Reserve() error = %v, want ErrInvalidConfiguration", err)
	}
}

func TestStandardAdmissionControllerRecoversExpiredReservations(t *testing.T) {
	startedAt := time.Date(2026, time.August, 4, 13, 0, 0, 0, time.UTC)
	runner := venat.NewDevelopment()
	controller := StandardAdmissionController{
		Runner: runner,
		Now:    func() time.Time { return startedAt },
		TTL:    time.Minute,
	}
	decision, err := controller.Reserve(context.Background(), RunAdmissionRequest{
		AgentID: "agent-1", RunID: "run-expired",
		Governance: api.GovernancePolicy{MaxConcurrentRuns: 1},
	})
	if err != nil || !decision.Allowed {
		t.Fatalf("Reserve() decision=%#v error=%v", decision, err)
	}
	recovered, err := controller.RecoverExpired(context.Background(), decision.Reservation.ExpiresAt.Add(time.Minute), 10)
	if err != nil {
		t.Fatalf("RecoverExpired() error = %v", err)
	}
	if len(recovered) != 1 || recovered[0].State != api.AdmissionExpired || recovered[0].Version != 2 {
		t.Fatalf("recovered reservations = %#v", recovered)
	}
	stored, err := runner.LoadAdmissionReservation(context.Background(), decision.Reservation.ID)
	if err != nil || stored.State != api.AdmissionExpired {
		t.Fatalf("stored reservation=%#v error=%v", stored, err)
	}
}
