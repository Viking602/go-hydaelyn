package provider

import (
	"context"
	"errors"
	"io"
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

type failureOnlyStream struct {
	failure error
}

func (s *failureOnlyStream) Recv() (Event, error) {
	failure := s.failure
	s.failure = io.EOF
	return Event{}, failure
}
func (*failureOnlyStream) Close() error { return nil }

func TestOpenRetryingStream_RetriesTransientOpenFailureToMax(t *testing.T) {
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
	for index, current := range progress {
		if current.Attempt != index+1 || current.Max != DefaultMaxStreamRetries {
			t.Fatalf("progress[%d] = %#v", index, current)
		}
	}
}

func TestOpenRetryingStream_CustomPolicyApprovesOrVetoesBeforeObserver(t *testing.T) {
	t.Run("approve non-default error", func(t *testing.T) {
		permanent := errors.New("application-approved retry")
		opens := 0
		policyCalls := 0
		observerCalls := 0
		stream, err := OpenRetryingStream(context.Background(), func() (Stream, error) {
			opens++
			if opens == 1 {
				return nil, permanent
			}
			return NewSliceStream([]Event{{Kind: EventDone, StopReason: StopReasonComplete}}), nil
		}, StreamRetryOptions{
			Max:   2,
			Delay: func(int) time.Duration { return 0 },
			ShouldRetry: func(progress RetryProgress) bool {
				policyCalls++
				return progress.Attempt == 1 && progress.Max == 2 && errors.Is(progress.Cause, permanent)
			},
			Observer: func(RetryProgress) error {
				observerCalls++
				return nil
			},
		})
		if err != nil {
			t.Fatalf("OpenRetryingStream() error = %v", err)
		}
		if event, err := stream.Recv(); err != nil || event.Kind != EventDone {
			t.Fatalf("Recv() event = %#v, error = %v", event, err)
		}
		if opens != 2 || policyCalls != 1 || observerCalls != 1 {
			t.Fatalf("opens=%d policy=%d observer=%d", opens, policyCalls, observerCalls)
		}
	})

	t.Run("veto default retry", func(t *testing.T) {
		opens := 0
		observerCalls := 0
		_, err := OpenRetryingStream(context.Background(), func() (Stream, error) {
			opens++
			return nil, retryableTestError{}
		}, StreamRetryOptions{
			Delay:       func(int) time.Duration { return 0 },
			ShouldRetry: func(RetryProgress) bool { return false },
			Observer: func(RetryProgress) error {
				observerCalls++
				return nil
			},
		})
		if err == nil {
			t.Fatal("OpenRetryingStream() error = nil")
		}
		if opens != 1 || observerCalls != 0 {
			t.Fatalf("opens=%d observer=%d, want one open and no observer", opens, observerCalls)
		}
	})
}

func TestOpenRetryingStream_RetriesPreFirstEventReceiveFailure(t *testing.T) {
	opens := 0
	stream, err := OpenRetryingStream(context.Background(), func() (Stream, error) {
		opens++
		if opens == 1 {
			return &failureOnlyStream{failure: io.ErrUnexpectedEOF}, nil
		}
		return NewSliceStream([]Event{{Kind: EventDone, StopReason: StopReasonComplete}}), nil
	}, StreamRetryOptions{Max: 2, Delay: func(int) time.Duration { return 0 }})
	if err != nil {
		t.Fatalf("OpenRetryingStream() error = %v", err)
	}
	if event, recvErr := stream.Recv(); recvErr != nil || event.Kind != EventDone {
		t.Fatalf("Recv() event = %#v, error = %v", event, recvErr)
	}
	if opens != 2 {
		t.Fatalf("stream opens = %d, want 2", opens)
	}
}

func TestOpenRetryingStream_ReturnsNonRetryablePartialErrorWithoutReopen(t *testing.T) {
	opens := 0
	cause := retryableTestError{}
	stream, err := OpenRetryingStream(context.Background(), func() (Stream, error) {
		opens++
		return &eventThenFailureStream{failure: cause}, nil
	}, StreamRetryOptions{Delay: func(int) time.Duration { return 0 }})
	if err != nil {
		t.Fatal(err)
	}
	if event, recvErr := stream.Recv(); recvErr != nil || event.Text != "partial" {
		t.Fatalf("first Recv() event=%#v error=%v", event, recvErr)
	}
	_, recvErr := stream.Recv()
	var partial *PartialStreamError
	if !errors.As(recvErr, &partial) || !errors.As(recvErr, new(retryableTestError)) {
		t.Fatalf("second Recv() error=%v, want PartialStreamError preserving cause", recvErr)
	}
	if partial.Retryable() || IsRetryableError(partial) {
		t.Fatalf("partial error is retryable: %v", partial)
	}
	if opens != 1 {
		t.Fatalf("stream opens=%d, want one after partial response", opens)
	}
}

func TestOpenRetryingStream_PreservesSameCallPartialEvent(t *testing.T) {
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
	event, recvErr := stream.Recv()
	var partial *PartialStreamError
	if event.Text != "partial" || !errors.As(recvErr, &partial) || !errors.Is(recvErr, io.ErrUnexpectedEOF) {
		t.Fatalf("Recv() event=%#v error=%v", event, recvErr)
	}
	if opens != 1 {
		t.Fatalf("stream opens=%d, want one after partial response", opens)
	}
}

func TestOpenRetryingStream_ConvertsPostEmissionErrorEventToPartialFailure(t *testing.T) {
	opens := 0
	stream, err := OpenRetryingStream(context.Background(), func() (Stream, error) {
		opens++
		return &scriptedRetryStream{results: []retryRecvResult{
			{event: Event{Kind: EventTextDelta, Text: "partial"}},
			{event: Event{Kind: EventError, Err: retryableTestError{}}},
		}}, nil
	}, StreamRetryOptions{Delay: func(int) time.Duration { return 0 }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("first Recv() error = %v", err)
	}
	event, recvErr := stream.Recv()
	var partial *PartialStreamError
	if event.Kind != "" || !errors.As(recvErr, &partial) {
		t.Fatalf("second Recv() event=%#v error=%v", event, recvErr)
	}
	if opens != 1 {
		t.Fatalf("stream opens=%d, want 1", opens)
	}
}

type retryRecvResult struct {
	event Event
	err   error
}

type scriptedRetryStream struct {
	results []retryRecvResult
	index   int
}

func (stream *scriptedRetryStream) Recv() (Event, error) {
	if stream.index >= len(stream.results) {
		return Event{}, io.EOF
	}
	result := stream.results[stream.index]
	stream.index++
	return result.event, result.err
}
func (*scriptedRetryStream) Close() error { return nil }

func TestOpenRetryingStream_AcceptsEmptyTerminalResponse(t *testing.T) {
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

func TestOpenRetryingStream_ObserverVetoAndSuggestedDelay(t *testing.T) {
	stop := errors.New("observer veto")
	var progress RetryProgress
	opens := 0
	_, err := OpenRetryingStream(context.Background(), func() (Stream, error) {
		opens++
		return nil, retryableTestError{delay: 3 * time.Second}
	}, StreamRetryOptions{
		Delay:    func(int) time.Duration { return 0 },
		MaxDelay: 2 * time.Second,
		Observer: func(current RetryProgress) error {
			progress = current
			return stop
		},
	})
	if !errors.Is(err, stop) {
		t.Fatalf("OpenRetryingStream() error=%v, want observer veto", err)
	}
	if progress.Delay != 2*time.Second || progress.Attempt != 1 || opens != 1 {
		t.Fatalf("retry progress=%#v opens=%d", progress, opens)
	}
}

func TestOpenRetryingStream_ContextIsNonOverridableHardStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	policyCalls := 0
	observerCalls := 0
	opens := 0
	_, err := OpenRetryingStream(ctx, func() (Stream, error) {
		opens++
		return nil, retryableTestError{}
	}, StreamRetryOptions{
		Delay: func(int) time.Duration { return 0 },
		ShouldRetry: func(RetryProgress) bool {
			policyCalls++
			return true
		},
		Observer: func(RetryProgress) error {
			observerCalls++
			return nil
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("OpenRetryingStream() error=%v, want context.Canceled", err)
	}
	if opens != 1 || policyCalls != 0 || observerCalls != 0 {
		t.Fatalf("opens=%d policy=%d observer=%d", opens, policyCalls, observerCalls)
	}

	opens = 0
	_, err = OpenRetryingStream(context.Background(), func() (Stream, error) {
		opens++
		return nil, context.DeadlineExceeded
	}, StreamRetryOptions{ShouldRetry: func(RetryProgress) bool {
		policyCalls++
		return true
	}})
	if !errors.Is(err, context.DeadlineExceeded) || opens != 1 || policyCalls != 0 {
		t.Fatalf("deadline error=%v opens=%d policy=%d", err, opens, policyCalls)
	}
}
