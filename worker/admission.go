package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/Viking602/venat"
	"github.com/Viking602/venat/api"
)

var ErrAdmissionControllerMissing = errors.New("worker: admission controller missing")

const defaultAdmissionTTL = 15 * time.Minute

// RunAdmissionRequest carries one definition's aggregate governance values to
// an AdmissionController before the run is dispatched.
type RunAdmissionRequest struct {
	AgentID      string
	AgentVersion string
	RunID        string
	Governance   api.GovernancePolicy
	TTL          time.Duration
}

// AdmissionController owns the reserve and lifecycle operations used by a run
// coordinator. Custom controllers may add application policy before delegating
// to Runner's atomic store-backed methods.
type AdmissionController interface {
	Reserve(context.Context, RunAdmissionRequest) (api.AdmissionDecision, error)
	Transition(context.Context, api.AdmissionTransition) (api.AdmissionDecision, error)
}

// AdmissionRecoveryController reconciles expired reservations after a process
// restart. It is optional for custom AdmissionController implementations.
type AdmissionRecoveryController interface {
	RecoverExpired(context.Context, time.Time, int) ([]api.AdmissionReservation, error)
}

// StandardAdmissionController maps AgentDefinition governance directly onto
// Runner's atomic AdmissionReservationStore contract.
type StandardAdmissionController struct {
	Runner *venat.Runner
	Now    func() time.Time
	TTL    time.Duration
}

// RequiresAdmission reports whether governance configures an aggregate limit.
func RequiresAdmission(governance api.GovernancePolicy) bool {
	return governance.MaxConcurrentRuns > 0 ||
		governance.Quota.MaxRunsPerWindow > 0 ||
		governance.Quota.MaxCredits > 0 ||
		governance.PauseOnExcessFailures > 0
}

// Reserve atomically reserves aggregate capacity. With no aggregate limits it
// returns Allowed without creating a durable reservation.
func (c StandardAdmissionController) Reserve(ctx context.Context, request RunAdmissionRequest) (api.AdmissionDecision, error) {
	if !RequiresAdmission(request.Governance) {
		return api.AdmissionDecision{Allowed: true}, nil
	}
	if c.Runner == nil {
		return api.AdmissionDecision{}, ErrRunnerMissing
	}
	if request.AgentID == "" || request.RunID == "" {
		return api.AdmissionDecision{}, fmt.Errorf("worker: admission agent and run ids are required: %w", api.ErrInvalidCommand)
	}
	if request.Governance.Quota.MaxCredits > 0 && request.Governance.Budget.MaxCredits <= 0 {
		return api.AdmissionDecision{}, fmt.Errorf("worker: aggregate credit quota requires budget.maxCredits: %w", api.ErrInvalidConfiguration)
	}
	now := c.now()
	ttl := c.ttl(request)
	decision, err := c.Runner.ReserveAdmission(ctx, api.AdmissionRequest{
		ReservationID:   admissionReservationID(request.AgentID, request.RunID),
		AgentID:         request.AgentID,
		AgentVersion:    request.AgentVersion,
		RunID:           request.RunID,
		Limits:          admissionLimits(request.Governance),
		ReservedCredits: max(request.Governance.Budget.MaxCredits, 0),
		RequestedAt:     now,
		ExpiresAt:       now.Add(ttl),
	})
	if err != nil {
		return api.AdmissionDecision{}, fmt.Errorf("worker: reserve admission for run %q: %w", request.RunID, err)
	}
	return decision, nil
}

// Transition delegates one expected-version lifecycle operation to Runner.
func (c StandardAdmissionController) Transition(ctx context.Context, transition api.AdmissionTransition) (api.AdmissionDecision, error) {
	if c.Runner == nil {
		return api.AdmissionDecision{}, ErrRunnerMissing
	}
	if transition.At.IsZero() {
		transition.At = c.now()
	}
	decision, err := c.Runner.TransitionAdmission(ctx, transition)
	if err != nil {
		return api.AdmissionDecision{}, fmt.Errorf("worker: transition admission %q to %q: %w", transition.ReservationID, transition.To, err)
	}
	return decision, nil
}

// RecoverExpired marks every due non-terminal reservation expired using its
// current version. Concurrent lifecycle winners are skipped.
func (c StandardAdmissionController) RecoverExpired(ctx context.Context, at time.Time, limit int) ([]api.AdmissionReservation, error) {
	if c.Runner == nil {
		return nil, ErrRunnerMissing
	}
	if at.IsZero() {
		at = c.now()
	} else {
		at = at.UTC()
	}
	candidates, err := c.Runner.ListAdmissionReservations(ctx, api.AdmissionReservationSelector{
		States:        []api.AdmissionState{api.AdmissionReserved, api.AdmissionActive, api.AdmissionSuspended},
		ExpiresBefore: at,
		Limit:         limit,
	})
	if err != nil {
		return nil, fmt.Errorf("worker: list expired admissions: %w", err)
	}
	recovered := make([]api.AdmissionReservation, 0, len(candidates))
	for _, candidate := range candidates {
		transitionAt := at
		if transitionAt.Before(candidate.UpdatedAt) {
			transitionAt = candidate.UpdatedAt
		}
		decision, transitionErr := c.Transition(ctx, api.AdmissionTransition{
			ReservationID:   candidate.ID,
			ExpectedVersion: candidate.Version,
			To:              api.AdmissionExpired,
			At:              transitionAt,
		})
		if transitionErr != nil {
			if errors.Is(transitionErr, api.ErrNotFound) || errors.Is(transitionErr, api.ErrInvalidTransition) {
				continue
			}
			return recovered, transitionErr
		}
		if !decision.Allowed {
			continue
		}
		recovered = append(recovered, decision.Reservation)
	}
	return recovered, nil
}

func (c StandardAdmissionController) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c StandardAdmissionController) ttl(request RunAdmissionRequest) time.Duration {
	ttl := request.TTL
	if ttl <= 0 {
		ttl = c.TTL
	}
	if ttl <= 0 {
		ttl = defaultAdmissionTTL
	}
	if request.Governance.Budget.MaxRuntime > ttl {
		ttl = request.Governance.Budget.MaxRuntime
	}
	return ttl
}

func admissionLimits(governance api.GovernancePolicy) api.AdmissionLimits {
	return api.AdmissionLimits{
		Window:                governance.Quota.Window,
		MaxConcurrentRuns:     governance.MaxConcurrentRuns,
		MaxRunsPerWindow:      governance.Quota.MaxRunsPerWindow,
		MaxCredits:            governance.Quota.MaxCredits,
		PauseOnExcessFailures: governance.PauseOnExcessFailures,
	}
}

func admissionReservationID(agentID, runID string) string {
	digest := sha256.Sum256([]byte(agentID + "\x00" + runID))
	return "admission-" + hex.EncodeToString(digest[:])
}
