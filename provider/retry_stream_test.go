package provider

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type retryableTestError struct {
	delay time.Duration
}

func (e retryableTestError) Error() string             { return "transient provider failure" }
func (e retryableTestError) Retryable() bool           { return true }
func (e retryableTestError) RetryDelay() time.Duration { return e.delay }

type eventThenFailureStream struct {
	received bool
	failure  error
}

func (s *eventThenFailureStream) Recv() (Event, error) {
	if !s.received {
		s.received = true
		return Event{Kind: EventTextDelta, Text: "partial"}, nil
	}
	return Event{}, s.failure
}
func (*eventThenFailureStream) Close() error { return nil }

type eventAndFailureStream struct {
	event    Event
	failure  error
	received bool
}

func (s *eventAndFailureStream) Recv() (Event, error) {
	if s.received {
		return Event{}, io.EOF
	}
	s.received = true
	return s.event, s.failure
}
func (*eventAndFailureStream) Close() error { return nil }

func TestOpenRetryingStreamRetriesTransientOpenFailure(t *testing.T) {
	attempts := 0
	var progress []RetryProgress
	stream, err := OpenRetryingStream(context.Background(), func() (Stream, error) {
		attempts++
		return nil, io.ErrUnexpectedEOF
	}, StreamRetryOptions{
		Delay: func(int) time.Duration { return 0 },
		Observer: func(current RetryProgress) error {
			progress = append(progress, current)
			return nil
		},
	})
	if stream != nil || err == nil || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("OpenRetryingStream() stream=%v error=%v", stream, err)
	}
	if attempts != DefaultMaxStreamRetries+1 || len(progress) != DefaultMaxStreamRetries {
		t.Fatalf("attempts=%d progress=%d", attempts, len(progress))
	}
}

func TestOpenRetryingStreamRefusesReplayAfterEmission(t *testing.T) {
	opens := 0
	stream, err := OpenRetryingStream(context.Background(), func() (Stream, error) {
		opens++
		return &eventThenFailureStream{failure: retryableTestError{}}, nil
	}, StreamRetryOptions{Delay: func(int) time.Duration { return 0 }})
	if err != nil {
		t.Fatal(err)
	}
	if event, recvErr := stream.Recv(); recvErr != nil || event.Text != "partial" {
		t.Fatalf("first Recv() event=%#v error=%v", event, recvErr)
	}
	_, recvErr := stream.Recv()
	if recvErr == nil || !strings.Contains(recvErr.Error(), "refusing unsafe replay") {
		t.Fatalf("second Recv() error=%v", recvErr)
	}
	if opens != 1 {
		t.Fatalf("stream opens=%d, want one after partial response", opens)
	}
}

func TestOpenRetryingStreamRefusesSameCallReplayAfterEmission(t *testing.T) {
	opens := 0
	stream, err := OpenRetryingStream(context.Background(), func() (Stream, error) {
		opens++
		return &eventAndFailureStream{
			event: Event{Kind: EventTextDelta, Text: "partial"}, failure: io.ErrUnexpectedEOF,
		}, nil
	}, StreamRetryOptions{Delay: func(int) time.Duration { return 0 }})
	if err != nil {
		t.Fatal(err)
	}
	_, recvErr := stream.Recv()
	if recvErr == nil || !strings.Contains(recvErr.Error(), "refusing unsafe replay") {
		t.Fatalf("Recv() error=%v, want unsafe replay refusal", recvErr)
	}
	if opens != 1 {
		t.Fatalf("stream opens=%d, want one after partial response", opens)
	}
}

func TestOpenRetryingStreamAcceptsEmptyTerminalResponse(t *testing.T) {
	opens := 0
	stream, err := OpenRetryingStream(context.Background(), func() (Stream, error) {
		opens++
		return NewSliceStream([]Event{{Kind: EventDone, StopReason: StopReasonComplete}}), nil
	}, StreamRetryOptions{Delay: func(int) time.Duration { return 0 }})
	if err != nil {
		t.Fatal(err)
	}
	event, recvErr := stream.Recv()
	if recvErr != nil || event.Kind != EventDone {
		t.Fatalf("first Recv() event=%#v error=%v", event, recvErr)
	}
	if _, recvErr = stream.Recv(); !errors.Is(recvErr, io.EOF) {
		t.Fatalf("second Recv() error=%v, want io.EOF", recvErr)
	}
	if opens != 1 {
		t.Fatalf("stream opens=%d, want one after terminal event", opens)
	}
}

func TestOpenRetryingStreamHonorsTypedRetryDelay(t *testing.T) {
	stop := errors.New("stop before wait")
	var progress RetryProgress
	_, err := OpenRetryingStream(context.Background(), func() (Stream, error) {
		return nil, retryableTestError{delay: 3 * time.Second}
	}, StreamRetryOptions{
		Delay: func(int) time.Duration { return 0 },
		Observer: func(current RetryProgress) error {
			progress = current
			return stop
		},
	})
	if !errors.Is(err, stop) {
		t.Fatalf("OpenRetryingStream() error=%v, want observer stop", err)
	}
	if progress.Delay != 3*time.Second || progress.Attempt != 1 {
		t.Fatalf("retry progress=%#v", progress)
	}
}

func TestOpenRetryingStreamCapsProviderRetryDelay(t *testing.T) {
	stop := errors.New("stop before wait")
	var progress RetryProgress
	_, err := OpenRetryingStream(context.Background(), func() (Stream, error) {
		return nil, retryableTestError{delay: 24 * time.Hour}
	}, StreamRetryOptions{
		Delay:    func(int) time.Duration { return 0 },
		MaxDelay: 5 * time.Minute,
		Observer: func(current RetryProgress) error {
			progress = current
			return stop
		},
	})
	if !errors.Is(err, stop) {
		t.Fatalf("OpenRetryingStream() error=%v, want observer stop", err)
	}
	if progress.Delay != 5*time.Minute {
		t.Fatalf("retry delay=%v, want 5m cap", progress.Delay)
	}
}
