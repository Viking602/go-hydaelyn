package stream

import (
	"context"
	"errors"
)

// Sink is the push interface a producer (the agent loop) emits Frames to.
// Implementations must be safe to call from the goroutine that drives the
// provider stream; concurrent Emit safety is implementation-specific.
type Sink interface {
	Emit(ctx context.Context, frame Frame) error
}

// SinkFunc adapts a plain function to a Sink.
type SinkFunc func(ctx context.Context, frame Frame) error

// Emit implements Sink.
func (f SinkFunc) Emit(ctx context.Context, frame Frame) error {
	if f == nil {
		return nil
	}
	return f(ctx, frame)
}

// Broadcast fans one stream out to several sinks (e.g. a UI renderer, an
// audit recorder, and a guardrail at once). Emit delivers to every sink
// and joins their errors rather than stopping at the first failure, so a
// single slow or failing consumer cannot starve the others. Context
// cancellation short-circuits before the next sink.
type Broadcast struct {
	sinks []Sink
}

// NewBroadcast builds a Broadcast over the given sinks. Nil sinks are
// dropped.
func NewBroadcast(sinks ...Sink) *Broadcast {
	filtered := make([]Sink, 0, len(sinks))
	for _, sink := range sinks {
		if sink != nil {
			filtered = append(filtered, sink)
		}
	}
	return &Broadcast{sinks: filtered}
}

// Emit implements Sink.
func (b *Broadcast) Emit(ctx context.Context, frame Frame) error {
	if b == nil {
		return nil
	}
	var errs []error
	for _, sink := range b.sinks {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		if err := sink.Emit(ctx, frame); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
