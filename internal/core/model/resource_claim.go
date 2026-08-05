package model

import "time"

type ResourceClaimMode string

const (
	ResourceClaimShared    ResourceClaimMode = "shared"
	ResourceClaimExclusive ResourceClaimMode = "exclusive"
)

type ResourceClaimState string

const (
	ResourceClaimActive   ResourceClaimState = "active"
	ResourceClaimReleased ResourceClaimState = "released"
	ResourceClaimExpired  ResourceClaimState = "expired"
)

type ResourceClaimDenialReason string

const (
	ResourceClaimDeniedConflict        ResourceClaimDenialReason = "conflict"
	ResourceClaimDeniedVersionConflict ResourceClaimDenialReason = "version_conflict"
)

type ResourceClaimSpec struct {
	ID   string
	Key  string
	Mode ResourceClaimMode
}

type ResourceClaim struct {
	ID        string
	Key       string
	Mode      ResourceClaimMode
	RunID     string
	TaskID    string
	LeaseID   string
	HolderID  string
	State     ResourceClaimState
	Version   uint64
	CreatedAt time.Time
	UpdatedAt time.Time
	ExpiresAt time.Time
}

type ResourceClaimRequest struct {
	RunID       string
	TaskID      string
	LeaseID     string
	HolderID    string
	Claims      []ResourceClaimSpec
	RequestedAt time.Time
	ExpiresAt   time.Time
}

type ResourceClaimTransition struct {
	ClaimID         string
	ExpectedVersion uint64
	To              ResourceClaimState
	At              time.Time
	ExpiresAt       time.Time
}

type ResourceClaimTransitionRequest struct {
	Transitions []ResourceClaimTransition
}

type ResourceClaimDecision struct {
	Acquired  bool
	Reason    ResourceClaimDenialReason
	Claims    []ResourceClaim
	Conflicts []ResourceClaim
}

type ResourceClaimSelector struct {
	IDs           []string
	Keys          []string
	RunIDs        []string
	TaskIDs       []string
	LeaseIDs      []string
	HolderIDs     []string
	Modes         []ResourceClaimMode
	States        []ResourceClaimState
	ExpiresBefore time.Time
	Limit         int
}
