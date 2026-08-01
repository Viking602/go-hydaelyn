package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
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

// RetryObserver receives each retry decision. Returning an error stops retrying.
type RetryObserver func(RetryProgress) error

// StreamRetryOptions configures retries before a stream emits any content.
type StreamRetryOptions struct {
	Max      int
	Delay    func(int) time.Duration
	MaxDelay time.Duration
	Observer RetryObserver
}

// OpenRetryingStream retries transient stream-open and pre-emission receive
// failures. Once any response content has been emitted, replay is refused and
// the transient error is returned for durable task-level continuation.
func OpenRetryingStream(ctx context.Context, open func() (Stream, error), options StreamRetryOptions) (Stream, error) {
	options = normalizedStreamRetryOptions(options)
	stream, retries, err := openStreamWithRetry(ctx, open, options, 0)
	if err != nil {
		return nil, err
	}
	return &retryingStream{ctx: ctx, current: stream, open: open, options: options, retries: retries}, nil
}

type retryingStream struct {
	ctx     context.Context
	current Stream
	open    func() (Stream, error)
	options StreamRetryOptions
	retries int
	emitted bool
	closed  bool
}

func (s *retryingStream) Recv() (Event, error) {
	for {
		if s.closed {
			return Event{}, fmt.Errorf("provider stream is closed")
		}
		event, recvErr := s.current.Recv()
		cause := recvErr
		if event.Kind == EventError && event.Err != nil {
			cause = event.Err
		}
		if event.Kind == EventDone {
			s.emitted = true
			return event, nil
		}
		if event.Kind != "" && event.Kind != EventError {
			s.emitted = true
		}
		if s.emitted && errors.Is(recvErr, io.EOF) {
			return event, recvErr
		}
		if !IsRetryableError(cause) {
			if recvErr == nil && event.Kind != EventError && event.Kind != EventDone {
				s.emitted = true
			}
			return event, recvErr
		}
		if s.emitted {
			return streamRetryFailure(event, fmt.Errorf("provider connection reset after partial response; refusing unsafe replay: %w", cause))
		}
		if s.retries >= s.options.Max {
			return streamRetryFailure(event, fmt.Errorf("provider stream failed after %d retries: %w", s.options.Max, cause))
		}
		_ = s.current.Close()
		s.retries++
		if err := reportStreamRetryAndWait(s.ctx, s.options, s.retries, cause); err != nil {
			return Event{}, err
		}
		next, retries, err := openStreamWithRetry(s.ctx, s.open, s.options, s.retries)
		s.retries = retries
		if err != nil {
			return streamRetryFailure(event, err)
		}
		s.current = next
	}
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
		if !IsRetryableError(err) || retries >= options.Max {
			if IsRetryableError(err) {
				err = fmt.Errorf("provider stream failed after %d retries: %w", options.Max, err)
			}
			return nil, retries, err
		}
		retries++
		if err := reportStreamRetryAndWait(ctx, options, retries, err); err != nil {
			return nil, retries, err
		}
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

func reportStreamRetryAndWait(ctx context.Context, options StreamRetryOptions, attempt int, cause error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	delay := min(max(time.Duration(0), options.Delay(attempt), SuggestedRetryDelay(cause)), options.MaxDelay)
	if options.Observer != nil {
		if err := options.Observer(RetryProgress{Attempt: attempt, Max: options.Max, Delay: delay, Cause: cause}); err != nil {
			return err
		}
	}
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func streamRetryFailure(event Event, err error) (Event, error) {
	if event.Kind == EventError {
		event.Err = err
		return event, nil
	}
	return Event{}, err
}
