package testbackend

import (
	"bytes"
	"context"
	"time"

	"github.com/Viking602/venat/durable"
)

func (backend *Backend) StartAttempt(ctx context.Context, request durable.StartAttemptRequest) (durable.AttemptStart, error) {
	if !validExecutionID(request.ExecutionID) || request.Lease.OwnerID == "" || request.Lease.Token == 0 || request.OperationID == "" || zeroHash(request.InputHash) || (request.Kind != durable.AttemptKindModel && request.Kind != durable.AttemptKindTool) {
		return durable.AttemptStart{}, contextOrValidation(ctx, attemptError(request.ExecutionID, request.OperationID, 0, durable.ErrInvalidArgument))
	}
	var started durable.AttemptStart
	err := backend.withRecord(ctx, request.ExecutionID, request.OperationID, func(record *executionRecord, now time.Time) error {
		if err := requireActiveLease(record, request.ExecutionID, request.Lease, now); err != nil {
			return attemptError(request.ExecutionID, request.OperationID, 0, durable.ErrLeaseLost)
		}
		attempts := record.attempts[request.OperationID]
		if len(attempts) == 0 {
			attempt := newAttempt(request, 1)
			record.attempts[request.OperationID] = []durable.Attempt{attempt}
			started = durable.AttemptStart{Attempt: cloneAttempt(attempt), Decision: durable.AttemptDecisionExecute}
			return nil
		}
		latest := &attempts[len(attempts)-1]
		if latest.Kind != request.Kind || latest.InputHash != request.InputHash {
			return attemptError(request.ExecutionID, request.OperationID, latest.Number, durable.ErrConflict)
		}
		switch latest.Status {
		case durable.AttemptStatusSucceeded, durable.AttemptStatusFailed:
			started = durable.AttemptStart{Attempt: cloneAttempt(*latest), Decision: durable.AttemptDecisionReplay}
		case durable.AttemptStatusUnknown:
			started = durable.AttemptStart{Attempt: cloneAttempt(*latest), Decision: durable.AttemptDecisionReconcile}
		case durable.AttemptStatusRunning:
			if latest.Lease != nil && *latest.Lease == request.Lease {
				started = durable.AttemptStart{Attempt: cloneAttempt(*latest), Decision: durable.AttemptDecisionExecute}
				break
			}
			latest.Status = durable.AttemptStatusUnknown
			latest.Lease = nil
			latest.Version++
			record.attempts[request.OperationID] = attempts
			started = durable.AttemptStart{Attempt: cloneAttempt(*latest), Decision: durable.AttemptDecisionReconcile}
		case durable.AttemptStatusAbandoned:
			attempt := newAttempt(request, latest.Number+1)
			attempts = append(attempts, attempt)
			record.attempts[request.OperationID] = attempts
			started = durable.AttemptStart{Attempt: cloneAttempt(attempt), Decision: durable.AttemptDecisionExecute}
		default:
			return attemptError(request.ExecutionID, request.OperationID, latest.Number, durable.ErrConflict)
		}
		return nil
	})
	return started, err
}

func newAttempt(request durable.StartAttemptRequest, number int) durable.Attempt {
	lease := request.Lease
	return durable.Attempt{
		ExecutionID: request.ExecutionID,
		OperationID: request.OperationID,
		Kind:        request.Kind,
		Number:      number,
		InputHash:   request.InputHash,
		Status:      durable.AttemptStatusRunning,
		Lease:       &lease,
		Version:     1,
	}
}

func (backend *Backend) FinishAttempt(ctx context.Context, request durable.FinishAttemptRequest) (durable.Attempt, error) {
	if err := validateAttemptMutation(request.ExecutionID, request.Lease, request.OperationID, request.AttemptNumber, request.ExpectedAttemptVersion); err != nil {
		return durable.Attempt{}, contextOrValidation(ctx, err)
	}
	var settled durable.Attempt
	err := backend.withRecord(ctx, request.ExecutionID, request.OperationID, func(record *executionRecord, now time.Time) error {
		if err := requireActiveLease(record, request.ExecutionID, request.Lease, now); err != nil {
			return attemptError(request.ExecutionID, request.OperationID, request.AttemptNumber, durable.ErrLeaseLost)
		}
		attempt, ok := attemptByNumber(record, request.OperationID, request.AttemptNumber)
		if !ok {
			return attemptError(request.ExecutionID, request.OperationID, request.AttemptNumber, durable.ErrNotFound)
		}
		if attempt.Status == durable.AttemptStatusSucceeded || attempt.Status == durable.AttemptStatusFailed {
			if attempt.Version == request.ExpectedAttemptVersion+1 && bytes.Equal(attempt.Payload, request.Payload) && sameFailure(attempt.Failure, request.Failure) {
				settled = cloneAttempt(*attempt)
				return nil
			}
			return attemptError(request.ExecutionID, request.OperationID, request.AttemptNumber, durable.ErrConflict)
		}
		if attempt.Status != durable.AttemptStatusRunning || attempt.Version != request.ExpectedAttemptVersion || attempt.Lease == nil || *attempt.Lease != request.Lease {
			return attemptError(request.ExecutionID, request.OperationID, request.AttemptNumber, durable.ErrConflict)
		}
		attempt.Status = durable.AttemptStatusSucceeded
		if request.Failure != nil {
			attempt.Status = durable.AttemptStatusFailed
		}
		attempt.Lease = nil
		attempt.Payload = bytes.Clone(request.Payload)
		attempt.Failure = cloneFailure(request.Failure)
		attempt.Version++
		settled = cloneAttempt(*attempt)
		return nil
	})
	return settled, err
}

func (backend *Backend) MarkAttemptUnknown(ctx context.Context, request durable.MarkAttemptUnknownRequest) (durable.Attempt, error) {
	if err := validateAttemptMutation(request.ExecutionID, request.Lease, request.OperationID, request.AttemptNumber, request.ExpectedAttemptVersion); err != nil {
		return durable.Attempt{}, contextOrValidation(ctx, err)
	}
	var settled durable.Attempt
	err := backend.withRecord(ctx, request.ExecutionID, request.OperationID, func(record *executionRecord, now time.Time) error {
		if err := requireActiveLease(record, request.ExecutionID, request.Lease, now); err != nil {
			return attemptError(request.ExecutionID, request.OperationID, request.AttemptNumber, durable.ErrLeaseLost)
		}
		attempt, ok := attemptByNumber(record, request.OperationID, request.AttemptNumber)
		if !ok {
			return attemptError(request.ExecutionID, request.OperationID, request.AttemptNumber, durable.ErrNotFound)
		}
		if attempt.Status == durable.AttemptStatusUnknown {
			if attempt.Version == request.ExpectedAttemptVersion+1 && bytes.Equal(attempt.Payload, request.Payload) && sameFailure(attempt.Failure, request.Failure) {
				settled = cloneAttempt(*attempt)
				return nil
			}
			return attemptError(request.ExecutionID, request.OperationID, request.AttemptNumber, durable.ErrConflict)
		}
		if attempt.Status != durable.AttemptStatusRunning || attempt.Version != request.ExpectedAttemptVersion || attempt.Lease == nil || *attempt.Lease != request.Lease {
			return attemptError(request.ExecutionID, request.OperationID, request.AttemptNumber, durable.ErrConflict)
		}
		attempt.Status = durable.AttemptStatusUnknown
		attempt.Lease = nil
		attempt.Payload = bytes.Clone(request.Payload)
		attempt.Failure = cloneFailure(request.Failure)
		attempt.Version++
		settled = cloneAttempt(*attempt)
		return nil
	})
	return settled, err
}

func (backend *Backend) ReconcileAttempt(ctx context.Context, request durable.ReconcileAttemptRequest) (durable.Attempt, error) {
	if err := validateReconcileRequest(request); err != nil {
		return durable.Attempt{}, contextOrValidation(ctx, err)
	}
	var reconciled durable.Attempt
	err := backend.withRecord(ctx, request.ExecutionID, request.OperationID, func(record *executionRecord, now time.Time) error {
		if leaseActive(record.execution.Lease, now) {
			return attemptError(request.ExecutionID, request.OperationID, request.AttemptNumber, durable.ErrBusy)
		}
		attempt, ok := attemptByNumber(record, request.OperationID, request.AttemptNumber)
		if !ok {
			return attemptError(request.ExecutionID, request.OperationID, request.AttemptNumber, durable.ErrNotFound)
		}
		if reconcileExact(*attempt, request) {
			reconciled = cloneAttempt(*attempt)
			return nil
		}
		if attempt.Status != durable.AttemptStatusUnknown || attempt.Version != request.ExpectedAttemptVersion {
			return attemptError(request.ExecutionID, request.OperationID, request.AttemptNumber, durable.ErrConflict)
		}
		switch request.Resolution {
		case durable.ReconcileResolutionSucceed:
			attempt.Status = durable.AttemptStatusSucceeded
			attempt.Payload = bytes.Clone(request.Payload)
			attempt.Failure = nil
		case durable.ReconcileResolutionFail:
			attempt.Status = durable.AttemptStatusFailed
			attempt.Payload = bytes.Clone(request.Payload)
			attempt.Failure = cloneFailure(request.Failure)
		case durable.ReconcileResolutionRetry:
			attempt.Status = durable.AttemptStatusAbandoned
			attempt.Payload = nil
			attempt.Failure = nil
		}
		attempt.Lease = nil
		attempt.Version++
		reconciled = cloneAttempt(*attempt)
		return nil
	})
	return reconciled, err
}

func validateAttemptMutation(executionID durable.ExecutionID, lease durable.LeaseRef, operationID string, number int, version uint64) error {
	if err := validateAttemptIdentity(executionID, operationID, number); err != nil {
		return err
	}
	if lease.OwnerID == "" || lease.Token == 0 || version == 0 {
		return attemptError(executionID, operationID, number, durable.ErrInvalidArgument)
	}
	return nil
}

func validateReconcileRequest(request durable.ReconcileAttemptRequest) error {
	if err := validateAttemptIdentity(request.ExecutionID, request.OperationID, request.AttemptNumber); err != nil {
		return err
	}
	if request.ExpectedAttemptVersion == 0 {
		return attemptError(request.ExecutionID, request.OperationID, request.AttemptNumber, durable.ErrInvalidArgument)
	}
	switch request.Resolution {
	case durable.ReconcileResolutionSucceed:
		if len(request.Payload) == 0 || request.Failure != nil {
			return attemptError(request.ExecutionID, request.OperationID, request.AttemptNumber, durable.ErrInvalidArgument)
		}
	case durable.ReconcileResolutionFail:
		if request.Failure == nil {
			return attemptError(request.ExecutionID, request.OperationID, request.AttemptNumber, durable.ErrInvalidArgument)
		}
	case durable.ReconcileResolutionRetry:
		if len(request.Payload) != 0 || request.Failure != nil {
			return attemptError(request.ExecutionID, request.OperationID, request.AttemptNumber, durable.ErrInvalidArgument)
		}
	default:
		return attemptError(request.ExecutionID, request.OperationID, request.AttemptNumber, durable.ErrInvalidArgument)
	}
	return nil
}

func reconcileExact(attempt durable.Attempt, request durable.ReconcileAttemptRequest) bool {
	if attempt.Version != request.ExpectedAttemptVersion+1 {
		return false
	}
	switch request.Resolution {
	case durable.ReconcileResolutionSucceed:
		return attempt.Status == durable.AttemptStatusSucceeded && bytes.Equal(attempt.Payload, request.Payload) && attempt.Failure == nil
	case durable.ReconcileResolutionFail:
		return attempt.Status == durable.AttemptStatusFailed && bytes.Equal(attempt.Payload, request.Payload) && sameFailure(attempt.Failure, request.Failure)
	case durable.ReconcileResolutionRetry:
		return attempt.Status == durable.AttemptStatusAbandoned && len(attempt.Payload) == 0 && attempt.Failure == nil
	default:
		return false
	}
}

func cloneFailure(failure *durable.FailureRecord) *durable.FailureRecord {
	if failure == nil {
		return nil
	}
	cloned := *failure
	return &cloned
}
