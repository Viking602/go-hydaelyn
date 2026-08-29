package durable

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidArgument      = errors.New("durable: invalid argument")
	ErrNotFound             = errors.New("durable: not found")
	ErrConflict             = errors.New("durable: conflict")
	ErrBusy                 = errors.New("durable: busy")
	ErrLeaseLost            = errors.New("durable: lease lost")
	ErrCorruptCheckpoint    = errors.New("durable: corrupt checkpoint")
	ErrReconcileRequired    = errors.New("durable: reconciliation required")
	ErrResumeTargetMismatch = errors.New("durable: resume target mismatch")
	ErrNotActive            = errors.New("durable: execution is not active in this runtime")
	ErrSuspended            = errors.New("durable: execution suspended")
	ErrClosed               = errors.New("durable: runtime closed")
)

// ExecutionError attributes a durable error to one execution and preserves its
// sentinel in the error chain.
type ExecutionError struct {
	ExecutionID ExecutionID
	Err         error
}

func (failure *ExecutionError) Error() string {
	if failure == nil {
		return "durable: execution error"
	}
	return fmt.Sprintf("durable: execution %q: %v", failure.ExecutionID, failure.Err)
}

func (failure *ExecutionError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.Err
}

// AttemptError attributes a durable error to one logical effect attempt and
// preserves its sentinel in the error chain.
type AttemptError struct {
	ExecutionID   ExecutionID
	OperationID   string
	AttemptNumber int
	Err           error
}

func (failure *AttemptError) Error() string {
	if failure == nil {
		return "durable: attempt error"
	}
	if failure.AttemptNumber > 0 {
		return fmt.Sprintf("durable: execution %q operation %q attempt %d: %v", failure.ExecutionID, failure.OperationID, failure.AttemptNumber, failure.Err)
	}
	return fmt.Sprintf("durable: execution %q operation %q: %v", failure.ExecutionID, failure.OperationID, failure.Err)
}

func (failure *AttemptError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.Err
}

// ReconcileRequiredError reports the exact uncertain attempts that must be
// resolved before an execution can continue.
type ReconcileRequiredError struct {
	Execution Execution
	Attempts  []Attempt
}

func (failure *ReconcileRequiredError) Error() string {
	if failure == nil {
		return ErrReconcileRequired.Error()
	}
	return fmt.Sprintf("durable: execution %q requires reconciliation for %d attempt(s)", failure.Execution.ID, len(failure.Attempts))
}

func (failure *ReconcileRequiredError) Unwrap() error {
	return ErrReconcileRequired
}

// ResumeTargetError reports the requested checkpoint facts and the claimed
// execution facts without changing the persisted continuation.
type ResumeTargetError struct {
	ExecutionID           ExecutionID
	Expected              ResumeTarget
	Actual                ResumeTarget
	AvailableOperationIDs []string
}

func (failure *ResumeTargetError) Error() string {
	if failure == nil {
		return ErrResumeTargetMismatch.Error()
	}
	return fmt.Sprintf(
		"durable: execution %q resume target mismatch: expected sequence=%d phase=%q operation=%q; actual sequence=%d phase=%q operation=%q available=%v",
		failure.ExecutionID,
		failure.Expected.CheckpointSequence,
		failure.Expected.Phase,
		failure.Expected.OperationID,
		failure.Actual.CheckpointSequence,
		failure.Actual.Phase,
		failure.Actual.OperationID,
		failure.AvailableOperationIDs,
	)
}

func (failure *ResumeTargetError) Unwrap() error {
	return ErrResumeTargetMismatch
}
