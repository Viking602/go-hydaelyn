// Package sse adapts a live stream.Frame stream onto a Server-Sent Events
// HTTP response. The Writer implements stream.Sink, so an application can
// pass it straight to agent.Engine.RunStream and pipe provider tokens to a
// browser end to end:
//
//	w, err := sse.NewWriter(rw)
//	if err != nil { ... }
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

	"github.com/Viking602/go-hydaelyn/stream"
)

// ErrNotFlushable is returned by NewWriter when the http.ResponseWriter
// cannot flush, which SSE requires to push each event promptly.
var ErrNotFlushable = errors.New("sse: response writer does not support flushing")

// Writer streams frames to an http.ResponseWriter as Server-Sent Events.
// Each frame becomes one SSE record: the frame Kind is the `event:` field
// and the JSON-encoded frame is the `data:` payload.
type Writer struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewWriter sets the SSE response headers, writes a 200, and returns a
// Writer. It fails with ErrNotFlushable if the writer cannot flush.
func NewWriter(w http.ResponseWriter) (*Writer, error) {
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
	return &Writer{w: w, flusher: flusher}, nil
}

// Emit implements stream.Sink. It encodes the frame, writes one SSE
// record, and flushes. A cancelled ctx short-circuits before writing.
func (sw *Writer) Emit(ctx context.Context, frame stream.Frame) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := encodeFrame(frame)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(sw.w, "event: %s\ndata: %s\n\n", frame.Kind, data); err != nil {
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
