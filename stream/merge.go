package stream

import (
	"context"
	"errors"
	"sync"
)

// Source is one labeled input to Merge.
type Source struct {
	// Label stamps Frame.Source on every frame from this input that does
	// not already carry one. For multi-agent fan-in this is typically the
	// AgentInstance ID.
	Label string
	// Frames is the input channel. A nil channel is ignored.
	Frames <-chan Frame
}

// Merge consumes frames from every source concurrently and forwards them
// to dst, stamping each frame's Source with the source label when it is
// empty. dst.Emit is serialized across sources, so dst need not be
// concurrency-safe. Merge returns once every source channel is drained or
// ctx is cancelled, joining any dst errors with the cancellation cause.
//
// Merge ships the fan-in primitive the later multi-agent scheduler work
// will use (parallel agents streaming into one consumer); it is not wired
// into any scheduler here.
func Merge(ctx context.Context, dst Sink, sources ...Source) error {
	if dst == nil || len(sources) == 0 {
		return nil
	}
	var (
		mu   sync.Mutex
		errs []error
		wg   sync.WaitGroup
	)
	emit := func(frame Frame) {
		mu.Lock()
		defer mu.Unlock()
		if err := dst.Emit(ctx, frame); err != nil {
			errs = append(errs, err)
		}
	}
	for _, src := range sources {
		if src.Frames == nil {
			continue
		}
		wg.Add(1)
		go func(source Source) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case frame, ok := <-source.Frames:
					if !ok {
						return
					}
					if frame.Source == "" {
						frame.Source = source.Label
					}
					emit(frame)
				}
			}
		}(src)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
