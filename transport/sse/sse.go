// Package sse adapts a live stream.Frame stream onto a Server-Sent Events
// HTTP response. The Writer implements stream.Sink, so an application can
// pass it straight to agent.Engine.RunStream and pipe provider tokens to a
// browser end to end:
//
//	w, err := sse.NewWriter(rw, r)
//	if err != nil { ... }
//	defer w.Close()
//	result := engine.RunStream(ctx, task, policy, w)
//
// Streaming stays a side-channel above the durable runner (final-state-only
// durability): the SSE response carries live frames, while the durable
// record of the run is still the final message the runner persists.
package sse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Viking602/venat/stream"
)

// ErrNotFlushable is returned by NewWriter when the http.ResponseWriter
// cannot flush, which SSE requires to push each event promptly.
var ErrNotFlushable = errors.New("sse: response writer does not support flushing")

// ErrClosed is returned by Emit (and Heartbeat) after Close has been called.
var ErrClosed = errors.New("sse: writer is closed")

// Writer streams frames to an http.ResponseWriter as Server-Sent Events.
// Each frame becomes one SSE record: the frame Kind is the `event:` field
// and the JSON-encoded frame is the `data:` payload.
type Writer struct {
	w       http.ResponseWriter
	flusher http.Flusher
	reqCtx  context.Context // the HTTP request's ctx; signals client disconnect

	mu     sync.Mutex
	closed bool
}

// NewWriter sets the SSE response headers, writes a 200, and returns a
// Writer. It fails with ErrNotFlushable if the writer cannot flush.
// The request's context is stored so Emit short-circuits the moment the
// client disconnects, independent of the engine's own ctx.
func NewWriter(w http.ResponseWriter, req *http.Request) (*Writer, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, ErrNotFlushable
	}
	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return &Writer{w: w, flusher: flusher, reqCtx: req.Context()}, nil
}

// Emit implements stream.Sink. It encodes the frame, writes one SSE
// record, and flushes. A cancelled ctx short-circuits before writing.
// If the HTTP request's context is done (client disconnected) Emit also
// short-circuits immediately. After Close, Emit returns ErrClosed.
func (sw *Writer) Emit(ctx context.Context, frame stream.Frame) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := sw.reqCtx.Err(); err != nil {
		return err
	}
	data, err := encodeFrame(frame)
	if err != nil {
		return err
	}

	// Hold the write lock across the write+flush so a concurrent
	// Heartbeat goroutine cannot interleave writes to sw.w.
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if sw.closed {
		return ErrClosed
	}
	if _, err := fmt.Fprintf(sw.w, "event: %s\ndata: %s\n\n", frame.Kind, data); err != nil {
		return err
	}
	sw.flusher.Flush()
	return nil
}

// Close flushes a final empty SSE comment (":\n\n") and marks the writer
// closed; subsequent Emit returns ErrClosed. Close is idempotent.
func (sw *Writer) Close() error {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if sw.closed {
		return nil
	}
	sw.closed = true
	_, _ = fmt.Fprint(sw.w, ":\n\n")
	sw.flusher.Flush()
	return nil
}

// Heartbeat launches a helper goroutine that emits an SSE comment
// (":\n\n") on the interval until ctx is done or Close is called, then
// returns. The caller owns the goroutine lifetime: cancel ctx (e.g. via
// the engine ctx, or a derived ctx tied to the request) to stop it.
//
// Heartbeat returns immediately; the goroutine keeps running in the
// background. It must not be called more than once per Writer.
func (sw *Writer) Heartbeat(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if sw.emitComment(ctx) != nil {
				return
			}
		}
	}()
}

// emitComment writes a heartbeat comment unless the writer is closed or
// either ctx is done. Returns ErrClosed (or the ctx error) so the
// heartbeat goroutine knows to stop.
func (sw *Writer) emitComment(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := sw.reqCtx.Err(); err != nil {
		return err
	}
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if sw.closed {
		return ErrClosed
	}
	if _, err := fmt.Fprint(sw.w, ":\n\n"); err != nil {
		return err
	}
	sw.flusher.Flush()
	return nil
}

// encodeFrame serializes a frame for the SSE data field. Error frames are
// special-cased because Frame.Err is not itself serializable.
func encodeFrame(frame stream.Frame) ([]byte, error) {
	if frame.Kind == stream.FrameError {
		message := ""
		if frame.Err != nil {
			message = frame.Err.Error()
		}
		return json.Marshal(struct {
			Kind  stream.FrameKind `json:"kind"`
			Error string           `json:"error,omitempty"`
		}{Kind: frame.Kind, Error: message})
	}
	return json.Marshal(frame)
}
