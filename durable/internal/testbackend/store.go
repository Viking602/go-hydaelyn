// Package testbackend provides the repository-private Backend implementation
// used by durable conformance and Runtime tests.
package testbackend

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Viking602/venat/durable"
)

var _ durable.Backend = (*Backend)(nil)

// Backend is private to the durable package tree and intentionally not a
// production reference backend.
type Backend struct {
	store *store
}

type store struct {
	mu         sync.Mutex
	offset     time.Duration
	executions map[durable.ExecutionID]*executionRecord
}

type executionRecord struct {
	execution durable.Execution
	attempts  map[string][]durable.Attempt
	claims    map[durable.ClaimID]claimRecord
	releases  map[leaseKey]durable.ReleaseResult
	suspends  map[suspendKey]durable.Execution
	finishes  map[finishKey]durable.Execution
	nextToken uint64
}

type claimRecord struct {
	mode     string
	ownerID  string
	leaseTTL time.Duration
	specHash [32]byte
	token    uint64
	start    *durable.StartResult
	resume   *durable.ResumeResult
}

type leaseKey struct {
	ownerID string
	token   uint64
}

type suspendKey struct {
	lease           leaseKey
	expectedVersion uint64
}

type finishKey struct {
	lease           leaseKey
	expectedVersion uint64
	resultHash      [32]byte
}

// New returns an empty test backend with a backend-owned clock.
func New() *Backend {
	return &Backend{store: &store{
		executions: make(map[durable.ExecutionID]*executionRecord),
	}}
}

// Reopen returns a new Backend handle over the same persisted test state.
func (backend *Backend) Reopen() *Backend {
	return &Backend{store: backend.store}
}

// Advance moves the backend-trusted clock forward.
func (backend *Backend) Advance(duration time.Duration) {
	backend.store.mu.Lock()
	backend.store.offset += duration
	backend.store.mu.Unlock()
}

func (backend *Backend) withRecord(ctx context.Context, executionID durable.ExecutionID, operationID string, call func(*executionRecord, time.Time) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	backend.store.mu.Lock()
	defer backend.store.mu.Unlock()
	record, ok := backend.store.executions[executionID]
	if !ok {
		if operationID != "" {
			return attemptError(executionID, operationID, 0, durable.ErrNotFound)
		}
		return executionError(executionID, durable.ErrNotFound)
	}
	return call(record, backend.store.trustedNow())
}

func (state *store) trustedNow() time.Time {
	return time.Now().UTC().Add(state.offset)
}

func executionError(executionID durable.ExecutionID, err error) error {
	return &durable.ExecutionError{ExecutionID: executionID, Err: err}
}

func attemptError(executionID durable.ExecutionID, operationID string, attemptNumber int, err error) error {
	return &durable.AttemptError{ExecutionID: executionID, OperationID: operationID, AttemptNumber: attemptNumber, Err: err}
}

func validExecutionID(executionID durable.ExecutionID) bool {
	return strings.TrimSpace(string(executionID)) != ""
}

func validLeaseInput(ownerID string, claimID durable.ClaimID, ttl time.Duration) bool {
	return strings.TrimSpace(ownerID) != "" && claimID != (durable.ClaimID{}) && ttl > 0
}

func leaseMatches(lease *durable.Lease, reference durable.LeaseRef) bool {
	return lease != nil && lease.OwnerID == reference.OwnerID && lease.Token == reference.Token
}

func leaseActive(lease *durable.Lease, now time.Time) bool {
	return lease != nil && lease.ExpiresAt.After(now)
}

func requireActiveLease(record *executionRecord, executionID durable.ExecutionID, reference durable.LeaseRef, now time.Time) error {
	if !leaseMatches(record.execution.Lease, reference) || !leaseActive(record.execution.Lease, now) {
		return executionError(executionID, durable.ErrLeaseLost)
	}
	return nil
}

func terminal(status durable.ExecutionStatus) bool {
	return status == durable.ExecutionStatusCompleted || status == durable.ExecutionStatusFailed
}

func cloneExecution(execution durable.Execution) durable.Execution {
	return cloneJSON(execution)
}

func cloneAttempt(attempt durable.Attempt) durable.Attempt {
	return cloneJSON(attempt)
}

func cloneAttempts(attempts []durable.Attempt) []durable.Attempt {
	if attempts == nil {
		return nil
	}
	cloned := make([]durable.Attempt, len(attempts))
	for index := range attempts {
		cloned[index] = cloneAttempt(attempts[index])
	}
	return cloned
}

func cloneStartResult(result durable.StartResult) durable.StartResult {
	result.Execution = cloneExecution(result.Execution)
	result.Reconcile = cloneAttempts(result.Reconcile)
	return result
}

func cloneResumeResult(result durable.ResumeResult) durable.ResumeResult {
	result.Execution = cloneExecution(result.Execution)
	result.Reconcile = cloneAttempts(result.Reconcile)
	return result
}

func cloneReleaseResult(result durable.ReleaseResult) durable.ReleaseResult {
	result.Execution = cloneExecution(result.Execution)
	result.Reconcile = cloneAttempts(result.Reconcile)
	return result
}

func cloneJSON[T any](value T) T {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	var cloned T
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		panic(err)
	}
	return cloned
}

func sameFailure(left, right *durable.FailureRecord) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func reconcileRunning(record *executionRecord, token uint64) []durable.Attempt {
	var reconciled []durable.Attempt
	for operationID, attempts := range record.attempts {
		for index := range attempts {
			attempt := &attempts[index]
			if attempt.Status != durable.AttemptStatusRunning || attempt.Lease == nil || (token != 0 && attempt.Lease.Token != token) {
				continue
			}
			attempt.Status = durable.AttemptStatusUnknown
			attempt.Lease = nil
			attempt.Version++
			reconciled = append(reconciled, cloneAttempt(*attempt))
		}
		record.attempts[operationID] = attempts
	}
	sortAttempts(reconciled)
	return reconciled
}

func reconcileForClaim(record *executionRecord) []durable.Attempt {
	reconcileRunning(record, 0)
	var reconciled []durable.Attempt
	for _, attempts := range record.attempts {
		for index := range attempts {
			if attempts[index].Status == durable.AttemptStatusUnknown {
				reconciled = append(reconciled, cloneAttempt(attempts[index]))
			}
		}
	}
	sortAttempts(reconciled)
	return reconciled
}

func sortAttempts(attempts []durable.Attempt) {
	sort.Slice(attempts, func(left, right int) bool {
		if attempts[left].OperationID != attempts[right].OperationID {
			return attempts[left].OperationID < attempts[right].OperationID
		}
		return attempts[left].Number < attempts[right].Number
	})
}

func attemptByNumber(record *executionRecord, operationID string, number int) (*durable.Attempt, bool) {
	attempts := record.attempts[operationID]
	for index := range attempts {
		if attempts[index].Number == number {
			return &attempts[index], true
		}
	}
	return nil, false
}

func validateAttemptIdentity(executionID durable.ExecutionID, operationID string, number int) error {
	if !validExecutionID(executionID) || strings.TrimSpace(operationID) == "" || number < 1 {
		return attemptError(executionID, operationID, number, durable.ErrInvalidArgument)
	}
	return nil
}

func zeroHash(hash [32]byte) bool { return hash == ([32]byte{}) }

func contextOrValidation(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}
