package durable

import (
	"fmt"

	"github.com/Viking602/venat/agent"
)

// ResumeTarget asserts persisted checkpoint facts before Engine code or an
// external effect can run. Zero fields are not asserted.
type ResumeTarget struct {
	CheckpointSequence uint64                  `json:"checkpointSequence,omitempty"`
	Phase              agent.ContinuationPhase `json:"phase,omitempty"`
	OperationID        string                  `json:"operationId,omitempty"`
}

// ResumeOptions configures factual preconditions for one resume claim.
type ResumeOptions struct {
	Target ResumeTarget `json:"target,omitempty"`
}

func (target ResumeTarget) empty() bool {
	return target.CheckpointSequence == 0 && target.Phase == "" && target.OperationID == ""
}

func resumeTargetMismatch(execution Execution, expected ResumeTarget) error {
	if expected.empty() {
		return nil
	}
	actual := ResumeTarget{}
	var available []string
	if execution.Checkpoint != nil {
		actual.CheckpointSequence = execution.Checkpoint.Sequence
		actual.Phase = execution.Checkpoint.Continuation.Phase
		available = pendingOperationIDs(execution.Checkpoint.Continuation)
		if len(available) == 1 {
			actual.OperationID = available[0]
		} else if expected.OperationID != "" && containsOperationID(available, expected.OperationID) {
			actual.OperationID = expected.OperationID
		}
	}

	matches := true
	if expected.CheckpointSequence != 0 && expected.CheckpointSequence != actual.CheckpointSequence {
		matches = false
	}
	if expected.Phase != "" && expected.Phase != actual.Phase {
		matches = false
	}
	if expected.OperationID != "" && !containsOperationID(available, expected.OperationID) {
		matches = false
	}
	if matches {
		return nil
	}
	return &ResumeTargetError{
		ExecutionID:           execution.ID,
		Expected:              expected,
		Actual:                actual,
		AvailableOperationIDs: append([]string(nil), available...),
	}
}

func pendingOperationIDs(continuation agent.Continuation) []string {
	switch continuation.Phase {
	case agent.ContinuationReady:
		return []string{fmt.Sprintf("turn:%d:model", continuation.NextOperationTurn)}
	case agent.ContinuationModelComplete:
		for index := len(continuation.Messages) - 1; index >= 0; index-- {
			calls := continuation.Messages[index].ToolCalls
			if len(calls) == 0 {
				continue
			}
			operationIDs := make([]string, len(calls))
			for callIndex := range calls {
				operationIDs[callIndex] = calls[callIndex].OperationID
			}
			return operationIDs
		}
	}
	return nil
}

func containsOperationID(operationIDs []string, operationID string) bool {
	for _, candidate := range operationIDs {
		if candidate == operationID {
			return true
		}
	}
	return false
}
