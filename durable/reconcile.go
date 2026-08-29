package durable

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Viking602/venat/message"
)

// Reconcile applies an explicit resolution to one unknown effect without
// claiming or running the execution.
func (runtime *Runtime) Reconcile(ctx context.Context, executionID ExecutionID, operationID string, reconciliation Reconciliation) error {
	if err := validateRuntimeCall(ctx, executionID); err != nil {
		return err
	}
	if strings.TrimSpace(operationID) == "" || reconciliation.AttemptNumber < 1 || reconciliation.AttemptVersion == 0 {
		return attemptRuntimeError(executionID, operationID, reconciliation.AttemptNumber, ErrInvalidArgument)
	}
	runtime.mu.Lock()
	if runtime.closed {
		runtime.mu.Unlock()
		return executionRuntimeError(executionID, ErrClosed)
	}
	if _, active := runtime.active[executionID]; active {
		runtime.mu.Unlock()
		return executionRuntimeError(executionID, ErrBusy)
	}
	runtime.mu.Unlock()

	execution, err := runtime.backend.LoadExecution(ctx, executionID)
	if err != nil {
		return backendExecutionOperationError("load execution for reconciliation", executionID, err)
	}
	if execution.ID != executionID {
		return executionRuntimeError(executionID, ErrConflict)
	}
	if err := validateExecutionCheckpoint(execution, executionID); err != nil {
		return err
	}
	kind, err := operationKind(operationID)
	if err != nil {
		return attemptRuntimeError(executionID, operationID, reconciliation.AttemptNumber, err)
	}
	payload, failure, err := reconciliationPayload(execution, operationID, kind, reconciliation)
	if err != nil {
		return attemptRuntimeError(executionID, operationID, reconciliation.AttemptNumber, errors.Join(ErrInvalidArgument, err))
	}
	_, err = runtime.backend.ReconcileAttempt(ctx, ReconcileAttemptRequest{
		ExecutionID:            executionID,
		OperationID:            operationID,
		AttemptNumber:          reconciliation.AttemptNumber,
		ExpectedAttemptVersion: reconciliation.AttemptVersion,
		Resolution:             reconciliation.Resolution,
		Payload:                payload,
		Failure:                failure,
	})
	return backendOperationError("reconcile attempt", err)
}

func reconciliationPayload(execution Execution, operationID string, kind AttemptKind, reconciliation Reconciliation) ([]byte, *FailureRecord, error) {
	switch kind {
	case AttemptKindModel:
		return modelReconciliationPayload(reconciliation)
	case AttemptKindTool:
		return toolReconciliationPayload(execution, operationID, reconciliation)
	default:
		return nil, nil, fmt.Errorf("unknown attempt kind %q", kind)
	}
}

func modelReconciliationPayload(reconciliation Reconciliation) ([]byte, *FailureRecord, error) {
	if reconciliation.ToolResult != nil {
		return nil, nil, errors.New("model reconciliation contains a tool result")
	}
	switch reconciliation.Resolution {
	case ReconcileResolutionSucceed:
		if reconciliation.Failure != nil {
			return nil, nil, errors.New("successful model reconciliation contains failure")
		}
		if err := validateSuccessfulModelEvents(reconciliation.ModelEvents); err != nil {
			return nil, nil, err
		}
		payload, err := encodeModelAttempt(reconciliation.ModelEvents, nil)
		return payload, nil, err
	case ReconcileResolutionFail:
		if reconciliation.Failure == nil {
			return nil, nil, errors.New("failed model reconciliation omits failure")
		}
		if err := validateFailedModelEvents(reconciliation.ModelEvents); err != nil {
			return nil, nil, err
		}
		failure := cloneFailureRecord(reconciliation.Failure)
		payload, err := encodeModelAttempt(reconciliation.ModelEvents, failure)
		return payload, failure, err
	case ReconcileResolutionRetry:
		if len(reconciliation.ModelEvents) != 0 || reconciliation.Failure != nil {
			return nil, nil, errors.New("retry model reconciliation contains output or failure")
		}
		return nil, nil, nil
	default:
		return nil, nil, fmt.Errorf("unknown reconciliation resolution %q", reconciliation.Resolution)
	}
}

func toolReconciliationPayload(execution Execution, operationID string, reconciliation Reconciliation) ([]byte, *FailureRecord, error) {
	if len(reconciliation.ModelEvents) != 0 {
		return nil, nil, errors.New("tool reconciliation contains model events")
	}
	call, err := checkpointToolCall(execution, operationID)
	if err != nil {
		return nil, nil, err
	}
	switch reconciliation.Resolution {
	case ReconcileResolutionSucceed:
		if reconciliation.Failure != nil || reconciliation.ToolResult == nil {
			return nil, nil, errors.New("successful tool reconciliation requires only a tool result")
		}
		result, err := normalizeReconciledToolResult(call, *reconciliation.ToolResult)
		if err != nil {
			return nil, nil, err
		}
		payload, err := encodeToolAttempt(result, nil)
		return payload, nil, err
	case ReconcileResolutionFail:
		if reconciliation.Failure == nil {
			return nil, nil, errors.New("failed tool reconciliation omits failure")
		}
		result := message.ToolResult{}
		if reconciliation.ToolResult != nil {
			result, err = normalizeReconciledToolResult(call, *reconciliation.ToolResult)
			if err != nil {
				return nil, nil, err
			}
		}
		failure := cloneFailureRecord(reconciliation.Failure)
		payload, err := encodeToolAttempt(result, failure)
		return payload, failure, err
	case ReconcileResolutionRetry:
		if reconciliation.ToolResult != nil || reconciliation.Failure != nil {
			return nil, nil, errors.New("retry tool reconciliation contains output or failure")
		}
		return nil, nil, nil
	default:
		return nil, nil, fmt.Errorf("unknown reconciliation resolution %q", reconciliation.Resolution)
	}
}

func operationKind(operationID string) (AttemptKind, error) {
	var turn int
	if count, err := fmt.Sscanf(operationID, "turn:%d:model", &turn); err == nil && count == 1 && turn >= 0 && operationID == fmt.Sprintf("turn:%d:model", turn) {
		return AttemptKindModel, nil
	}
	var call int
	if count, err := fmt.Sscanf(operationID, "turn:%d:call:%d", &turn, &call); err == nil && count == 2 && turn >= 0 && call >= 0 && operationID == fmt.Sprintf("turn:%d:call:%d", turn, call) {
		return AttemptKindTool, nil
	}
	return "", fmt.Errorf("invalid operation ID %q", operationID)
}

func checkpointToolCall(execution Execution, operationID string) (message.ToolCall, error) {
	if execution.Checkpoint == nil {
		return message.ToolCall{}, errors.New("tool reconciliation requires a checkpoint")
	}
	var found *message.ToolCall
	for _, current := range execution.Checkpoint.Continuation.Messages {
		for _, call := range current.ToolCalls {
			if call.OperationID != operationID {
				continue
			}
			if found != nil {
				return message.ToolCall{}, fmt.Errorf("checkpoint repeats operation ID %q", operationID)
			}
			cloned := call
			found = &cloned
		}
	}
	if found == nil {
		return message.ToolCall{}, fmt.Errorf("checkpoint omits operation ID %q", operationID)
	}
	return *found, nil
}

func normalizeReconciledToolResult(call message.ToolCall, result message.ToolResult) (message.ToolResult, error) {
	if result.ToolCallID == "" {
		result.ToolCallID = call.ID
	} else if result.ToolCallID != call.ID {
		return message.ToolResult{}, fmt.Errorf("tool result call ID %q conflicts with %q", result.ToolCallID, call.ID)
	}
	if result.Name == "" {
		result.Name = call.Name
	} else if result.Name != call.Name {
		return message.ToolResult{}, fmt.Errorf("tool result name %q conflicts with %q", result.Name, call.Name)
	}
	return result, nil
}
