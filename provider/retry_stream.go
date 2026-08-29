package provider

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	DefaultMaxStreamRetries    = 5
	DefaultMaxStreamRetryDelay = 30 * time.Second
)

// RetryProgress describes a provider connection retry before backoff starts.
type RetryProgress struct {
	Attempt int
	Max     int
	Delay   time.Duration
	Cause   error
}

// RetryObservable is implemented by drivers that expose SDK retry progress.
type RetryObservable interface {
	SetRetryObserver(RetryObserver)
}

// RetryDelayConfigurable is implemented by drivers whose provider-suggested
// retry delay can be capped by host policy.
type RetryDelayConfigurable interface {
	SetMaxRetryDelay(time.Duration)
}

// RetryObserver receives each approved retry before its backoff. Returning an
// error vetoes that retry.
type RetryObserver func(RetryProgress) error

// StreamRetryOptions configures retries before a stream emits any content.
type StreamRetryOptions struct {
	Max         int
	Delay       func(int) time.Duration
	MaxDelay    time.Duration
	ShouldRetry func(RetryProgress) bool
	Observer    RetryObserver
}

// PartialStreamError reports a receive or terminal failure after valid output
// was delivered. Reopening the stream would duplicate an ambiguous effect.
type PartialStreamError struct {
	Cause error
}

func (failure *PartialStreamError) Error() string {
	if failure == nil || failure.Cause == nil {
		return "provider stream failed after partial output"
	}
	return fmt.Sprintf("provider stream failed after partial output: %v", failure.Cause)
}

func (failure *PartialStreamError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.Cause
}

// Retryable always returns false because partial output is an unsafe replay
// boundary regardless of the underlying transport classification.
func (*PartialStreamError) Retryable() bool { return false }

// OpenRetryingStream retries stream-open and pre-emission receive failures.
// Once valid output has been emitted, every failure is returned as a
// PartialStreamError and the stream is never reopened.
func OpenRetryingStream(ctx context.Context, open func() (Stream, error), options StreamRetryOptions) (Stream, error) {
	options = normalizedStreamRetryOptions(options)
	stream, retries, err := openStreamWithRetry(ctx, open, options, 0)
	if err != nil {
		return nil, err
	}
	return &retryingStream{ctx: ctx, current: stream, open: open, options: options, retries: retries}, nil
}

type retryingStream struct {
	ctx      context.Context
	current  Stream
	open     func() (Stream, error)
	options  StreamRetryOptions
	retries  int
	emitted  bool
	terminal bool
	closed   bool
}

// Identity follows the currently active stream so a pre-emission retry can
// report the provider/model that ultimately completed the turn.
func (s *retryingStream) Identity() StreamIdentity {
	if identified, ok := s.current.(IdentifiedStream); ok {
		return identified.Identity()
	}
	return StreamIdentity{}
}

func (s *retryingStream) Recv() (Event, error) {
	for {
		if s.closed {
			return Event{}, fmt.Errorf("provider stream is closed")
		}
		if s.terminal {
			return s.current.Recv()
		}

		event, recvErr := s.current.Recv()
		classified, retry, resultErr := s.classifyReceived(event, recvErr)
		if !retry {
			return classified, resultErr
		}
		cause := resultErr
		if contextRetryStop(cause) {
			return Event{}, cause
		}
		if s.retries >= s.options.Max {
			return streamRetryFailure(event, fmt.Errorf("provider stream failed after %d retries: %w", s.options.Max, cause))
		}
		approved, err := approveStreamRetryAndWait(s.ctx, s.options, s.retries+1, cause)
		if err != nil {
			return Event{}, err
		}
		if !approved {
			if event.Kind == EventError {
				s.terminal = true
				return event, recvErr
			}
			return event, cause
		}

		_ = s.current.Close()
		s.retries++
		next, retries, err := openStreamWithRetry(s.ctx, s.open, s.options, s.retries)
		s.retries = retries
		if err != nil {
			return streamRetryFailure(event, err)
		}
		s.current = next
	}
}

func (s *retryingStream) classifyReceived(event Event, recvErr error) (Event, bool, error) {
	cause := recvErr
	if event.Kind == EventError && event.Err != nil {
		cause = event.Err
	}
	if event.Kind == EventDone {
		s.terminal = true
		if recvErr != nil {
			return Event{}, false, &PartialStreamError{Cause: recvErr}
		}
		return event, false, nil
	}
	if event.Kind != "" && event.Kind != EventError {
		s.emitted = true
		if recvErr != nil {
			s.terminal = true
			return event, false, &PartialStreamError{Cause: recvErr}
		}
		return event, false, nil
	}
	if s.emitted {
		return s.classifyPartialFailure(event, recvErr, cause)
	}
	if cause == nil {
		if event.Kind == EventError {
			s.terminal = true
		}
		return event, false, recvErr
	}
	return event, true, cause
}

func (s *retryingStream) classifyPartialFailure(event Event, recvErr, cause error) (Event, bool, error) {
	if cause == nil {
		if event.Kind != EventError {
			return event, false, recvErr
		}
		cause = errors.New("provider returned an error event after partial output")
	}
	s.terminal = true
	if event.Kind == EventError {
		event = Event{}
	}
	return event, false, &PartialStreamError{Cause: cause}
}

func (s *retryingStream) Close() error {
	s.closed = true
	if s.current == nil {
		return nil
	}
	return s.current.Close()
}

func openStreamWithRetry(ctx context.Context, open func() (Stream, error), options StreamRetryOptions, retries int) (Stream, int, error) {
	for {
		stream, err := open()
		if err == nil {
			return stream, retries, nil
		}
		if contextRetryStop(err) {
			return nil, retries, err
		}
		if retries >= options.Max {
			return nil, retries, fmt.Errorf("provider stream failed after %d retries: %w", options.Max, err)
		}
		approved, approvalErr := approveStreamRetryAndWait(ctx, options, retries+1, err)
		if approvalErr != nil {
			return nil, retries, approvalErr
		}
		if !approved {
			return nil, retries, err
		}
		retries++
	}
}

func normalizedStreamRetryOptions(options StreamRetryOptions) StreamRetryOptions {
	if options.Max <= 0 {
		options.Max = DefaultMaxStreamRetries
	}
	if options.MaxDelay <= 0 {
		options.MaxDelay = DefaultMaxStreamRetryDelay
	}
	if options.Delay == nil {
		options.Delay = func(attempt int) time.Duration {
			shift := min(max(attempt-1, 0), 30)
			return min(200*time.Millisecond*time.Duration(1<<shift), 2*time.Second)
		}
	}
	return options
}

func approveStreamRetryAndWait(ctx context.Context, options StreamRetryOptions, attempt int, cause error) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if contextRetryStop(cause) {
		return false, nil
	}
	progress := RetryProgress{
		Attempt: attempt,
		Max:     options.Max,
		Delay:   min(max(time.Duration(0), options.Delay(attempt), SuggestedRetryDelay(cause)), options.MaxDelay),
		Cause:   cause,
	}
	approved := IsRetryableError(cause)
	if options.ShouldRetry != nil {
		approved = options.ShouldRetry(progress)
	}
	if !approved {
		return false, nil
	}
	if options.Observer != nil {
		if err := options.Observer(progress); err != nil {
			return false, err
		}
	}
	if progress.Delay <= 0 {
		return true, nil
	}
	timer := time.NewTimer(progress.Delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-timer.C:
		return true, nil
	}
}

func contextRetryStop(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func streamRetryFailure(event Event, err error) (Event, error) {
	if event.Kind == EventError {
		event.Err = err
		return event, nil
	}
	return event, err
}
