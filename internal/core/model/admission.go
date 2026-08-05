package model

import "time"

type AdmissionState string

const (
	AdmissionReserved  AdmissionState = "reserved"
	AdmissionActive    AdmissionState = "active"
	AdmissionSuspended AdmissionState = "suspended"
	AdmissionSettled   AdmissionState = "settled"
	AdmissionReleased  AdmissionState = "released"
	AdmissionExpired   AdmissionState = "expired"
)

type AdmissionDenialReason string

const (
	AdmissionDeniedConcurrency     AdmissionDenialReason = "concurrency_limit"
	AdmissionDeniedRunWindow       AdmissionDenialReason = "run_window_limit"
	AdmissionDeniedCredits         AdmissionDenialReason = "credit_limit"
	AdmissionDeniedFailureBreaker  AdmissionDenialReason = "failure_breaker"
	AdmissionDeniedVersionConflict AdmissionDenialReason = "version_conflict"
)

type AdmissionLimits struct {
	Window                time.Duration
	MaxConcurrentRuns     int
	MaxRunsPerWindow      int
	MaxCredits            int64
	PauseOnExcessFailures int
}

type AdmissionReservation struct {
	ID              string
	AgentID         string
	AgentVersion    string
	RunID           string
	State           AdmissionState
	Limits          AdmissionLimits
	ReservedCredits int64
	ConsumedCredits int64
	Failed          bool
	Version         uint64
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ActivatedAt     time.Time
	SettledAt       time.Time
	ExpiresAt       time.Time
}

type AdmissionRequest struct {
	ReservationID   string
	AgentID         string
	AgentVersion    string
	RunID           string
	Limits          AdmissionLimits
	ReservedCredits int64
	RequestedAt     time.Time
	ExpiresAt       time.Time
}

type AdmissionUsage struct {
	ConcurrentRuns   int
	RunsInWindow     int
	ReservedCredits  int64
	CommittedCredits int64
	TrailingFailures int
}

type AdmissionDecision struct {
	Allowed     bool
	Reason      AdmissionDenialReason
	Usage       AdmissionUsage
	Reservation AdmissionReservation
}

type AdmissionTransition struct {
	ReservationID   string
	ExpectedVersion uint64
	To              AdmissionState
	At              time.Time
	ExpiresAt       time.Time
	ConsumedCredits int64
	Failed          bool
}

type AdmissionReservationSelector struct {
	AgentIDs      []string
	RunIDs        []string
	States        []AdmissionState
	Since         time.Time
	ExpiresBefore time.Time
	Limit         int
}
