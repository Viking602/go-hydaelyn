package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/tool"
)

type modelAttemptInterceptor struct {
	active *activeExecution
}

func (interceptor modelAttemptInterceptor) Stream(ctx context.Context, next provider.Driver, request provider.Request) (provider.Stream, error) {
	if err := interceptor.active.beginEffect(); err != nil {
		return nil, err
	}
	effectOwned := true
	defer func() {
		if effectOwned {
			interceptor.active.endEffect()
		}
	}()
	if request.OperationID == "" {
		return nil, attemptRuntimeError(interceptor.active.id, request.OperationID, 0, ErrInvalidArgument)
	}
	inputHash, err := canonicalJSONHash(request)
	if err != nil {
		return nil, runtimeOperationError("hash model request", err)
	}
	execution := interceptor.active.snapshot()
	if execution.Lease == nil {
		return nil, executionRuntimeError(interceptor.active.id, ErrLeaseLost)
	}
	started, err := interceptor.active.runtime.backend.StartAttempt(ctx, StartAttemptRequest{
		ExecutionID: interceptor.active.id,
		Lease:       leaseReference(*execution.Lease),
		OperationID: request.OperationID,
		Kind:        AttemptKindModel,
		InputHash:   inputHash,
	})
	if err != nil {
		return nil, backendOperationError("start model attempt", err)
	}
	switch started.Decision {
	case AttemptDecisionReplay:
		stream, replayErr := replayModelAttempt(started.Attempt)
		return stream, runtimeOperationError("replay model attempt", replayErr)
	case AttemptDecisionReconcile:
		return nil, interceptor.active.reconcileError([]Attempt{started.Attempt})
	case AttemptDecisionExecute:
	default:
		return nil, attemptRuntimeError(interceptor.active.id, request.OperationID, started.Attempt.Number, ErrConflict)
	}

	stream, streamErr := openModelStream(ctx, next, request)
	if streamErr != nil {
		if errors.Is(streamErr, provider.ErrNotStarted) {
			failure := failureFromError(streamErr)
			payload, encodeErr := encodeModelAttempt(nil, failure)
			if encodeErr != nil {
				return nil, errors.Join(streamErr, encodeErr)
			}
			settleErr := finishAttempt(interceptor.active, started.Attempt, payload, failure)
			return nil, errors.Join(streamErr, settleErr)
		}
		failure := failureFromError(streamErr)
		payload, encodeErr := encodeModelAttempt(nil, failure)
		unknown, settleErr := markAttemptUnknown(interceptor.active, started.Attempt, payload, failure)
		if settleErr != nil {
			return nil, errors.Join(streamErr, encodeErr, settleErr)
		}
		return nil, errors.Join(streamErr, encodeErr, interceptor.active.reconcileError([]Attempt{unknown}))
	}
	if stream == nil {
		failure := &FailureRecord{Code: "nil_stream", Message: "provider returned a nil stream"}
		payload, encodeErr := encodeModelAttempt(nil, failure)
		unknown, settleErr := markAttemptUnknown(interceptor.active, started.Attempt, payload, failure)
		if settleErr != nil {
			return nil, errors.Join(errors.New(failure.Message), encodeErr, settleErr)
		}
		return nil, errors.Join(errors.New(failure.Message), encodeErr, interceptor.active.reconcileError([]Attempt{unknown}))
	}
	wrapped := &durableModelStream{active: interceptor.active, attempt: started.Attempt, next: stream}
	if identified, ok := stream.(provider.IdentifiedStream); ok {
		identity := identified.Identity()
		effectOwned = false
		return identifiedDurableModelStream{Stream: wrapped, identity: identity}, nil
	}
	effectOwned = false
	return wrapped, nil
}

func openModelStream(ctx context.Context, next provider.Driver, request provider.Request) (stream provider.Stream, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			stream = nil
			err = runtimeOperationError("open model stream", fmt.Errorf("%w: %v", agent.ErrPanicRecovered, recovered))
		}
	}()
	return next.Stream(ctx, request)
}

type identifiedDurableModelStream struct {
	provider.Stream
	identity provider.StreamIdentity
}

func (stream identifiedDurableModelStream) Identity() provider.StreamIdentity { return stream.identity }

type durableModelStream struct {
	active  *activeExecution
	attempt Attempt
	next    provider.Stream

	mu       sync.Mutex
	events   []provider.Event
	settled  bool
	closed   bool
	closeErr error
	endOnce  sync.Once
}

func (stream *durableModelStream) Recv() (provider.Event, error) {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.closed {
		return provider.Event{}, io.ErrClosedPipe
	}
	if stream.settled {
		return stream.next.Recv()
	}
	event, recvErr := stream.next.Recv()
	if event.Kind != "" {
		stream.events = append(stream.events, cloneProviderEvent(event))
	}
	if event.Kind == provider.EventDone || event.Kind == provider.EventError {
		failure := (*FailureRecord)(nil)
		validationErr := error(nil)
		if event.Kind == provider.EventDone {
			validationErr = validateSuccessfulModelEvents(stream.events)
		} else {
			failure = failureFromError(event.Err)
			if failure == nil {
				failure = &FailureRecord{Code: "provider_error", Message: "provider returned an error event"}
			}
			validationErr = validateFailedModelEvents(stream.events)
		}
		if validationErr != nil {
			failure = failureFromError(validationErr)
		}
		payload, encodeErr := encodeModelAttempt(stream.events, failure)
		if encodeErr != nil {
			unknown, unknownErr := markAttemptUnknown(stream.active, stream.attempt, nil, failureFromError(encodeErr))
			stream.finishEffect()
			stream.settled = true
			if unknownErr != nil {
				return provider.Event{}, errors.Join(validationErr, encodeErr, unknownErr)
			}
			return provider.Event{}, errors.Join(validationErr, encodeErr, stream.active.reconcileError([]Attempt{unknown}))
		}
		settleErr := finishAttempt(stream.active, stream.attempt, payload, failure)
		stream.finishEffect()
		stream.settled = true
		if settleErr != nil {
			return provider.Event{}, settleErr
		}
		if validationErr != nil {
			return provider.Event{}, validationErr
		}
		return event, recvErr
	}
	if recvErr != nil {
		failure := failureFromError(recvErr)
		payload, encodeErr := encodeModelAttempt(stream.events, failure)
		unknown, unknownErr := markAttemptUnknown(stream.active, stream.attempt, payload, failure)
		stream.finishEffect()
		stream.settled = true
		if unknownErr != nil {
			return event, errors.Join(recvErr, encodeErr, unknownErr)
		}
		return event, errors.Join(recvErr, encodeErr, stream.active.reconcileError([]Attempt{unknown}))
	}
	return event, nil
}

func (stream *durableModelStream) Close() error {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.closed {
		return stream.closeErr
	}
	stream.closed = true
	closeErr := stream.next.Close()
	if stream.settled {
		stream.closeErr = closeErr
		return closeErr
	}
	failure := failureFromError(closeErr)
	if failure == nil {
		failure = &FailureRecord{Code: "stream_closed", Message: "provider stream closed before a terminal event"}
	}
	payload, encodeErr := encodeModelAttempt(stream.events, failure)
	unknown, unknownErr := markAttemptUnknown(stream.active, stream.attempt, payload, failure)
	stream.finishEffect()
	stream.settled = true
	if unknownErr != nil {
		stream.closeErr = errors.Join(closeErr, encodeErr, unknownErr)
		return stream.closeErr
	}
	stream.closeErr = errors.Join(closeErr, encodeErr, stream.active.reconcileError([]Attempt{unknown}))
	return stream.closeErr
}

func (stream *durableModelStream) finishEffect() {
	stream.endOnce.Do(stream.active.endEffect)
}

type toolAttemptInterceptor struct {
	active *activeExecution
}

func (interceptor toolAttemptInterceptor) Execute(ctx context.Context, next tool.Driver, call tool.Call, sink tool.UpdateSink) (tool.Result, error) {
	if err := interceptor.active.beginEffect(); err != nil {
		return tool.Result{}, err
	}
	defer interceptor.active.endEffect()
	if call.OperationID == "" {
		return tool.Result{}, attemptRuntimeError(interceptor.active.id, call.OperationID, 0, ErrInvalidArgument)
	}
	inputHash, err := toolInputHash(call)
	if err != nil {
		return tool.Result{}, runtimeOperationError("hash tool call", err)
	}
	execution := interceptor.active.snapshot()
	if execution.Lease == nil {
		return tool.Result{}, executionRuntimeError(interceptor.active.id, ErrLeaseLost)
	}
	started, err := interceptor.active.runtime.backend.StartAttempt(ctx, StartAttemptRequest{
		ExecutionID: interceptor.active.id,
		Lease:       leaseReference(*execution.Lease),
		OperationID: call.OperationID,
		Kind:        AttemptKindTool,
		InputHash:   inputHash,
	})
	if err != nil {
		return tool.Result{}, backendOperationError("start tool attempt", err)
	}
	switch started.Decision {
	case AttemptDecisionReplay:
		result, failure, decodeErr := decodeToolAttempt(started.Attempt.Payload)
		if decodeErr != nil {
			return tool.Result{}, runtimeOperationError("replay tool attempt", decodeErr)
		}
		if failure != nil {
			return result, recordedFailureError(*failure)
		}
		return result, nil
	case AttemptDecisionReconcile:
		return tool.Result{}, interceptor.active.reconcileError([]Attempt{started.Attempt})
	case AttemptDecisionExecute:
	default:
		return tool.Result{}, attemptRuntimeError(interceptor.active.id, call.OperationID, started.Attempt.Number, ErrConflict)
	}

	result, executeErr := next.Execute(ctx, call, sink)
	failure := failureFromError(executeErr)
	payload, encodeErr := encodeToolAttempt(result, failure)
	if executeErr == nil && encodeErr == nil {
		return result, finishAttempt(interceptor.active, started.Attempt, payload, nil)
	}
	if executeErr != nil && errors.Is(executeErr, tool.ErrNotExecuted) && encodeErr == nil {
		return result, errors.Join(executeErr, finishAttempt(interceptor.active, started.Attempt, payload, failure))
	}
	unknown, unknownErr := markAttemptUnknown(interceptor.active, started.Attempt, payload, failure)
	if unknownErr != nil {
		return result, errors.Join(executeErr, encodeErr, unknownErr)
	}
	return result, errors.Join(executeErr, encodeErr, interceptor.active.reconcileError([]Attempt{unknown}))
}

func replayModelAttempt(attempt Attempt) (provider.Stream, error) {
	events, failure, err := decodeModelAttempt(attempt.Payload)
	if err != nil {
		return nil, err
	}
	switch attempt.Status {
	case AttemptStatusSucceeded:
		if failure != nil {
			return nil, errors.New("succeeded model attempt contains failure")
		}
		if err := validateSuccessfulModelEvents(events); err != nil {
			return nil, err
		}
	case AttemptStatusFailed:
		if failure == nil {
			return nil, errors.New("failed model attempt omits failure")
		}
		if err := validateFailedModelEvents(events); err != nil {
			return nil, err
		}
		if len(events) == 0 {
			return nil, recordedFailureError(*failure)
		}
		if events[len(events)-1].Kind != provider.EventError {
			return &failedModelReplayStream{events: events, failure: recordedFailureError(*failure)}, nil
		}
	default:
		return nil, fmt.Errorf("cannot replay model attempt status %q", attempt.Status)
	}
	return provider.NewSliceStream(events), nil
}

type failedModelReplayStream struct {
	events  []provider.Event
	index   int
	failure error
}

func (stream *failedModelReplayStream) Recv() (provider.Event, error) {
	if stream.index < len(stream.events) {
		event := stream.events[stream.index]
		stream.index++
		return event, nil
	}
	if stream.failure != nil {
		failure := stream.failure
		stream.failure = nil
		return provider.Event{}, failure
	}
	return provider.Event{}, io.EOF
}

func (*failedModelReplayStream) Close() error { return nil }

func finishAttempt(active *activeExecution, attempt Attempt, payload []byte, failure *FailureRecord) error {
	ctx, cancel := active.settlementContext()
	defer cancel()
	execution := active.snapshot()
	if execution.Lease == nil {
		return executionRuntimeError(active.id, ErrLeaseLost)
	}
	_, err := active.runtime.backend.FinishAttempt(ctx, FinishAttemptRequest{
		ExecutionID:            active.id,
		Lease:                  leaseReference(*execution.Lease),
		OperationID:            attempt.OperationID,
		AttemptNumber:          attempt.Number,
		ExpectedAttemptVersion: attempt.Version,
		Payload:                payload,
		Failure:                cloneFailureRecord(failure),
	})
	return backendOperationError("finish attempt", err)
}

func markAttemptUnknown(active *activeExecution, attempt Attempt, payload []byte, failure *FailureRecord) (Attempt, error) {
	ctx, cancel := active.settlementContext()
	defer cancel()
	execution := active.snapshot()
	if execution.Lease == nil {
		return Attempt{}, executionRuntimeError(active.id, ErrLeaseLost)
	}
	unknown, err := active.runtime.backend.MarkAttemptUnknown(ctx, MarkAttemptUnknownRequest{
		ExecutionID:            active.id,
		Lease:                  leaseReference(*execution.Lease),
		OperationID:            attempt.OperationID,
		AttemptNumber:          attempt.Number,
		ExpectedAttemptVersion: attempt.Version,
		Payload:                payload,
		Failure:                cloneFailureRecord(failure),
	})
	if err != nil {
		return Attempt{}, backendOperationError("mark attempt unknown", err)
	}
	return unknown, nil
}

func toolInputHash(call tool.Call) ([32]byte, error) {
	return canonicalJSONHash(struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
	}{Name: call.Name, Arguments: call.Arguments})
}

func attemptRuntimeError(executionID ExecutionID, operationID string, number int, err error) error {
	return &AttemptError{ExecutionID: executionID, OperationID: operationID, AttemptNumber: number, Err: err}
}
