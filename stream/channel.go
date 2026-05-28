package stream

import (
	"context"
	"errors"
	"iter"
	"sync"
)

// ErrClosed is returned by Channel.Emit after the channel has been closed.
var ErrClosed = errors.New("stream channel closed")

// Channel is the pull-side adapter over the push Sink model. A single
// producer (typically the agent loop) emits frames; a consumer ranges
// over Frames or Seq. The buffer bounds in-flight memory and provides
// natural backpressure: when the buffer is full Emit blocks until the
// consumer catches up or ctx is cancelled.
//
// Contract: Emit and Close are called by one producer goroutine and never
// concurrently with each other. Any number of values may be read from
// Frames; abandon a stream early by canceling the ctx passed to Emit.
type Channel struct {
	ch     chan Frame
	closed chan struct{}
	once   sync.Once
}

// NewChannel returns a Channel with the given buffer size (clamped to a
// minimum of zero, i.e. unbuffered).
func NewChannel(buffer int) *Channel {
	if buffer < 0 {
		buffer = 0
	}
	return &Channel{
		ch:     make(chan Frame, buffer),
		closed: make(chan struct{}),
	}
}

// Emit implements Sink. It blocks while the buffer is full, returning
// early if ctx is cancelled or the channel is closed.
func (c *Channel) Emit(ctx context.Context, frame Frame) error {
	select {
	case <-c.closed:
		return ErrClosed
	default:
	}
	select {
	case c.ch <- frame:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return ErrClosed
	}
}

// Close ends the stream. It is idempotent. After Close, Frames/Seq drain
// any buffered frames and then terminate, and further Emit calls return
// ErrClosed.
func (c *Channel) Close() {
	c.once.Do(func() {
		close(c.closed)
		close(c.ch)
	})
}

// Frames exposes the underlying receive channel for `for frame := range`
// consumption.
func (c *Channel) Frames() <-chan Frame {
	return c.ch
}

// Seq exposes the stream as a range-over-func iterator. Breaking out of
// the range stops iteration but does not close the producer; cancel the
// producer's ctx to release it.
func (c *Channel) Seq() iter.Seq[Frame] {
	return func(yield func(Frame) bool) {
		for frame := range c.ch {
			if !yield(frame) {
				return
			}
		}
	}
}
