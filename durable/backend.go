// Package durable provides optional crash recovery for one Agent execution
// through an application-supplied execution-semantic Backend.
package durable

import (
	"context"
	"time"

	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/tool"
)

// ExecutionID is an application-owned stable execution key.
type ExecutionID string

// ClaimID is one high-level claim command's stable 128-bit idempotency key.
type ClaimID [16]byte

// ExecutionStatus is the durable lifecycle state of one execution.
type ExecutionStatus string

const (
	ExecutionStatusRunning   ExecutionStatus = "running"
	ExecutionStatusSuspended ExecutionStatus = "suspended"
	ExecutionStatusCompleted ExecutionStatus = "completed"
	ExecutionStatusFailed    ExecutionStatus = "failed"
)

// ExecutionSpec is the immutable input contract for one execution.
type ExecutionSpec struct {
	Request      agent.Request      `json:"request"`
	OutputPolicy agent.OutputPolicy `json:"outputPolicy"`
}

// Execution is the complete durable state returned by Backend operations.
type Execution struct {
	ID         ExecutionID     `json:"id"`
	Spec       ExecutionSpec   `json:"spec"`
	SpecHash   [32]byte        `json:"specHash"`
	Status     ExecutionStatus `json:"status"`
	Version    uint64          `json:"version"`
	Lease      *Lease          `json:"lease,omitempty"`
	Checkpoint *Checkpoint     `json:"checkpoint,omitempty"`
	Result     *agent.Result   `json:"result,omitempty"`
	ResultHash [32]byte        `json:"resultHash,omitempty"`
}

// Checkpoint completely replaces the prior continuation at a monotonic
// sequence.
type Checkpoint struct {
	Sequence         uint64             `json:"sequence"`
	Continuation     agent.Continuation `json:"continuation"`
	ContinuationHash [32]byte           `json:"continuationHash"`
}

// Lease is a backend-timed execution claim.
type Lease struct {
	OwnerID   string    `json:"ownerId"`
	ClaimID   ClaimID   `json:"claimId"`
	Token     uint64    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// LeaseRef fences a mutation by owner and monotonic token.
type LeaseRef struct {
	OwnerID string `json:"ownerId"`
	Token   uint64 `json:"token"`
}

// StartResult reports creation or exact claim replay and attempts atomically
// made uncertain by the claim.
type StartResult struct {
	Execution Execution `json:"execution"`
	Created   bool      `json:"created"`
	Reconcile []Attempt `json:"reconcile,omitempty"`
}

// ResumeResult reports a claim and attempts atomically made uncertain by it.
type ResumeResult struct {
	Execution Execution `json:"execution"`
	Reconcile []Attempt `json:"reconcile,omitempty"`
}

// ReleaseResult reports release and attempts atomically made uncertain by it.
type ReleaseResult struct {
	Execution Execution `json:"execution"`
	Reconcile []Attempt `json:"reconcile,omitempty"`
}

// AttemptKind identifies the intercepted effect type.
type AttemptKind string

const (
	AttemptKindModel AttemptKind = "model"
	AttemptKindTool  AttemptKind = "tool"
)

// AttemptStatus is one logical effect attempt's durable state.
type AttemptStatus string

const (
	AttemptStatusRunning   AttemptStatus = "running"
	AttemptStatusSucceeded AttemptStatus = "succeeded"
	AttemptStatusFailed    AttemptStatus = "failed"
	AttemptStatusUnknown   AttemptStatus = "unknown"
	AttemptStatusAbandoned AttemptStatus = "abandoned"
)

// AttemptDecision tells Runtime whether to execute, replay, or reconcile.
type AttemptDecision string

const (
	AttemptDecisionExecute   AttemptDecision = "execute"
	AttemptDecisionReplay    AttemptDecision = "replay"
	AttemptDecisionReconcile AttemptDecision = "reconcile"
)

// Attempt is one versioned provider or tool effect record.
type Attempt struct {
	ExecutionID ExecutionID    `json:"executionId"`
	OperationID string         `json:"operationId"`
	Kind        AttemptKind    `json:"kind"`
	Number      int            `json:"number"`
	InputHash   [32]byte       `json:"inputHash"`
	Status      AttemptStatus  `json:"status"`
	Lease       *LeaseRef      `json:"lease,omitempty"`
	Version     uint64         `json:"version"`
	Payload     []byte         `json:"payload,omitempty"`
	Failure     *FailureRecord `json:"failure,omitempty"`
}

// AttemptStart combines the current attempt with its required action.
type AttemptStart struct {
	Attempt  Attempt         `json:"attempt"`
	Decision AttemptDecision `json:"decision"`
}

// FailureRecord is a durable, implementation-neutral failure fact.
type FailureRecord struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ReconcileResolution is the application's explicit decision for an unknown
// effect.
type ReconcileResolution string

const (
	ReconcileResolutionSucceed ReconcileResolution = "succeed"
	ReconcileResolutionFail    ReconcileResolution = "fail"
	ReconcileResolutionRetry   ReconcileResolution = "retry"
)

// Reconciliation is the typed public resolution input. Runtime encodes it to
// the Backend's opaque attempt payload.
type Reconciliation struct {
	AttemptNumber  int                 `json:"attemptNumber"`
	AttemptVersion uint64              `json:"attemptVersion"`
	Resolution     ReconcileResolution `json:"resolution"`
	ModelEvents    []provider.Event    `json:"modelEvents,omitempty"`
	ToolResult     *tool.Result        `json:"toolResult,omitempty"`
	Failure        *FailureRecord      `json:"failure,omitempty"`
}

// StartExecutionRequest atomically creates or claims an execution.
type StartExecutionRequest struct {
	ExecutionID ExecutionID   `json:"executionId"`
	OwnerID     string        `json:"ownerId"`
	ClaimID     ClaimID       `json:"claimId"`
	LeaseTTL    time.Duration `json:"leaseTtl"`
	Spec        ExecutionSpec `json:"spec"`
	SpecHash    [32]byte      `json:"specHash"`
}

// ResumeExecutionRequest claims an existing non-terminal execution.
type ResumeExecutionRequest struct {
	ExecutionID ExecutionID   `json:"executionId"`
	OwnerID     string        `json:"ownerId"`
	ClaimID     ClaimID       `json:"claimId"`
	LeaseTTL    time.Duration `json:"leaseTtl"`
}

// RenewExecutionRequest extends a current lease without advancing execution
// version.
type RenewExecutionRequest struct {
	ExecutionID ExecutionID   `json:"executionId"`
	Lease       LeaseRef      `json:"lease"`
	LeaseTTL    time.Duration `json:"leaseTtl"`
}

// SaveCheckpointRequest replaces the continuation using execution-version CAS.
type SaveCheckpointRequest struct {
	ExecutionID     ExecutionID `json:"executionId"`
	Lease           LeaseRef    `json:"lease"`
	ExpectedVersion uint64      `json:"expectedVersion"`
	Checkpoint      Checkpoint  `json:"checkpoint"`
}

// SuspendExecutionRequest suspends using execution-version CAS and releases
// the fenced lease.
type SuspendExecutionRequest struct {
	ExecutionID     ExecutionID `json:"executionId"`
	Lease           LeaseRef    `json:"lease"`
	ExpectedVersion uint64      `json:"expectedVersion"`
}

// FinishExecutionRequest commits a terminal Agent result using
// execution-version CAS.
type FinishExecutionRequest struct {
	ExecutionID     ExecutionID  `json:"executionId"`
	Lease           LeaseRef     `json:"lease"`
	ExpectedVersion uint64       `json:"expectedVersion"`
	Result          agent.Result `json:"result"`
	ResultHash      [32]byte     `json:"resultHash"`
}

// ReleaseExecutionRequest releases a fenced lease without changing status or
// execution version.
type ReleaseExecutionRequest struct {
	ExecutionID ExecutionID `json:"executionId"`
	Lease       LeaseRef    `json:"lease"`
}

// StartAttemptRequest starts or classifies one logical effect slot.
type StartAttemptRequest struct {
	ExecutionID ExecutionID `json:"executionId"`
	Lease       LeaseRef    `json:"lease"`
	OperationID string      `json:"operationId"`
	Kind        AttemptKind `json:"kind"`
	InputHash   [32]byte    `json:"inputHash"`
}

// FinishAttemptRequest settles a known effect outcome using attempt-version
// CAS. Failure nil means succeeded; non-nil means failed.
type FinishAttemptRequest struct {
	ExecutionID            ExecutionID    `json:"executionId"`
	Lease                  LeaseRef       `json:"lease"`
	OperationID            string         `json:"operationId"`
	AttemptNumber          int            `json:"attemptNumber"`
	ExpectedAttemptVersion uint64         `json:"expectedAttemptVersion"`
	Payload                []byte         `json:"payload,omitempty"`
	Failure                *FailureRecord `json:"failure,omitempty"`
}

// MarkAttemptUnknownRequest records an uncertain outcome using
// attempt-version CAS.
type MarkAttemptUnknownRequest struct {
	ExecutionID            ExecutionID    `json:"executionId"`
	Lease                  LeaseRef       `json:"lease"`
	OperationID            string         `json:"operationId"`
	AttemptNumber          int            `json:"attemptNumber"`
	ExpectedAttemptVersion uint64         `json:"expectedAttemptVersion"`
	Payload                []byte         `json:"payload,omitempty"`
	Failure                *FailureRecord `json:"failure,omitempty"`
}

// ReconcileAttemptRequest applies an explicit resolution without claiming the
// execution.
type ReconcileAttemptRequest struct {
	ExecutionID            ExecutionID         `json:"executionId"`
	OperationID            string              `json:"operationId"`
	AttemptNumber          int                 `json:"attemptNumber"`
	ExpectedAttemptVersion uint64              `json:"expectedAttemptVersion"`
	Resolution             ReconcileResolution `json:"resolution"`
	Payload                []byte              `json:"payload,omitempty"`
	Failure                *FailureRecord      `json:"failure,omitempty"`
}

// Backend owns atomic execution, lease, checkpoint, and effect-attempt
// semantics. It owns neither application schema nor connection lifecycle.
type Backend interface {
	StartExecution(context.Context, StartExecutionRequest) (StartResult, error)
	ResumeExecution(context.Context, ResumeExecutionRequest) (ResumeResult, error)
	LoadExecution(context.Context, ExecutionID) (Execution, error)
	RenewExecution(context.Context, RenewExecutionRequest) (Lease, error)
	SaveCheckpoint(context.Context, SaveCheckpointRequest) (Execution, error)
	SuspendExecution(context.Context, SuspendExecutionRequest) (Execution, error)
	FinishExecution(context.Context, FinishExecutionRequest) (Execution, error)
	ReleaseExecution(context.Context, ReleaseExecutionRequest) (ReleaseResult, error)
	StartAttempt(context.Context, StartAttemptRequest) (AttemptStart, error)
	FinishAttempt(context.Context, FinishAttemptRequest) (Attempt, error)
	MarkAttemptUnknown(context.Context, MarkAttemptUnknownRequest) (Attempt, error)
	ReconcileAttempt(context.Context, ReconcileAttemptRequest) (Attempt, error)
}
