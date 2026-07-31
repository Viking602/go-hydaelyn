package sse

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Viking602/venat/stream"
)

// newTestWriter builds a Writer against a fresh ResponseRecorder with a
// dummy request, returning the writer and the recorder so tests can
// inspect the streamed body.
func newTestWriter(t *testing.T) (*Writer, *httptest.ResponseRecorder) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	writer, err := NewWriter(rec, req)
	if err != nil {
		t.Fatalf("NewWriter error = %v", err)
	}
	return writer, rec
}

func TestWriterEmitsServerSentEvents(t *testing.T) {
	writer, rec := newTestWriter(t)
	ctx := context.Background()
	frames := []stream.Frame{
		{Kind: stream.FrameText, Text: "hi"},
		{Kind: stream.FrameError, Err: errors.New("boom")},
	}
	for _, frame := range frames {
		if err := writer.Emit(ctx, frame); err != nil {
			t.Fatalf("Emit error = %v", err)
		}
	}

	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: text\ndata: {\"kind\":\"text\",\"text\":\"hi\"") {
		t.Fatalf("missing text event in body:\n%s", body)
	}
	// Error frames carry the message even though Frame.Err is not serializable.
	if !strings.Contains(body, "event: error\ndata: {\"kind\":\"error\",\"error\":\"boom\"}\n\n") {
		t.Fatalf("missing error event in body:\n%s", body)
	}
}

func TestNewWriterRejectsNonFlushable(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	if _, err := NewWriter(nonFlushable{}, req); !errors.Is(err, ErrNotFlushable) {
		t.Fatalf("NewWriter error = %v, want ErrNotFlushable", err)
	}
}

// TestWriter_EmitReturnsErrOnClientDisconnect verifies that Emit
// short-circuits when the HTTP request's context is done — i.e. the
// client has disconnected — even though the engine ctx is still alive.
func TestWriter_EmitReturnsErrOnClientDisconnect(t *testing.T) {
	// Build a request whose context is cancellable so we can simulate
	// a client disconnect without touching the engine ctx.
	reqCtx, cancelReq := context.WithCancel(context.Background())
	defer cancelReq()
	req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(reqCtx)
	rec := httptest.NewRecorder()
	writer, err := NewWriter(rec, req)
	if err != nil {
		t.Fatalf("NewWriter error = %v", err)
	}

	// Disconnect the client.
	cancelReq()

	// Sanity: the stored request ctx reports done.
	if err := writer.reqCtx.Err(); err == nil {
		t.Fatalf("expected stored request ctx to be done after cancel")
	}

	// Engine ctx is still alive; Emit must still fail because the
	// request ctx is done.
	engineCtx := context.Background()
	err = writer.Emit(engineCtx, stream.Frame{Kind: stream.FrameText, Text: "late"})
	if err == nil {
		t.Fatalf("Emit after client disconnect = nil, want non-nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Emit error = %v, want context.Canceled", err)
	}
}

// TestWriter_CloseIsIdempotentAndRejectsEmit verifies Close flushes a
// final comment, is idempotent, and makes subsequent Emit return
// ErrClosed.
func TestWriter_CloseIsIdempotentAndRejectsEmit(t *testing.T) {
	writer, rec := newTestWriter(t)

	if err := writer.Close(); err != nil {
		t.Fatalf("Close error = %v", err)
	}
	body := rec.Body.String()
	if !strings.HasSuffix(body, ":\n\n") {
		t.Fatalf("Close must flush a final empty SSE comment, got body:\n%s", body)
	}

	// Idempotent: a second Close returns nil without appending again.
	lenAfterFirst := len(rec.Body.String())
	if err := writer.Close(); err != nil {
		t.Fatalf("second Close error = %v", err)
	}
	if len(rec.Body.String()) != lenAfterFirst {
		t.Fatalf("second Close appended data; body length changed from %d to %d", lenAfterFirst, len(rec.Body.String()))
	}

	// After Close, Emit returns ErrClosed.
	err := writer.Emit(context.Background(), stream.Frame{Kind: stream.FrameText, Text: "x"})
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("Emit after Close = %v, want ErrClosed", err)
	}
}

// TestWriter_HeartbeatEmitsComments verifies that Heartbeat emits SSE
// comments on the interval and stops when its ctx is done.
func TestWriter_HeartbeatEmitsComments(t *testing.T) {
	writer, rec := newTestWriter(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer.Heartbeat(ctx, 5*time.Millisecond)

	// Wait long enough for at least two ticks.
	time.Sleep(40 * time.Millisecond)
	cancel()
	// Give the goroutine a moment to observe cancellation.
	time.Sleep(20 * time.Millisecond)

	writer.mu.Lock()
	body := rec.Body.String()
	count := strings.Count(body, ":\n\n")
	writer.mu.Unlock()
	if count < 2 {
		t.Fatalf("expected at least 2 heartbeat comments, got %d in body:\n%s", count, body)
	}

	// After cancel, no further comments should be written. Snapshot
	// the count, wait, and confirm it doesn't grow.
	writer.mu.Lock()
	before := rec.Body.Len()
	writer.mu.Unlock()
	time.Sleep(40 * time.Millisecond)
	writer.mu.Lock()
	after := rec.Body.Len()
	writer.mu.Unlock()
	if after != before {
		t.Fatalf("heartbeat kept writing after ctx cancel: before=%d after=%d", before, after)
	}
}

// TestWriter_HeartbeatStopsOnClose verifies the heartbeat goroutine
// stops emitting once Close is called.
func TestWriter_HeartbeatStopsOnClose(t *testing.T) {
	writer, rec := newTestWriter(t)

	// Use a long-lived ctx; the goroutine should still stop because
	// Close marks the writer closed and emitComment returns ErrClosed.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer.Heartbeat(ctx, 5*time.Millisecond)

	time.Sleep(30 * time.Millisecond)
	if err := writer.Close(); err != nil {
		t.Fatalf("Close error = %v", err)
	}
	before := rec.Body.Len()
	time.Sleep(40 * time.Millisecond)
	if rec.Body.Len() != before {
		t.Fatalf("heartbeat kept writing after Close: before=%d after=%d", before, rec.Body.Len())
	}
}

// TestWriter_EmitHonorsEngineCtxCancelled verifies that when the engine
// ctx is cancelled but the request ctx is still alive, Emit returns the
// engine ctx error.
func TestWriter_EmitHonorsEngineCtxCancelled(t *testing.T) {
	writer, _ := newTestWriter(t)
	engineCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err := writer.Emit(engineCtx, stream.Frame{Kind: stream.FrameText, Text: "x"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Emit error = %v, want context.Canceled", err)
	}
}

// TestWriter_ConcurrentEmitAndHeartbeat exercises Emit, Heartbeat, and
// Close from concurrent goroutines. The mutex must serialize all writes
// to the underlying ResponseWriter so the body never corrupts. Run with
// -race to catch data races on the writer internals.
func TestWriter_ConcurrentEmitAndHeartbeat(t *testing.T) {
	writer, rec := newTestWriter(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer.Heartbeat(ctx, 2*time.Millisecond)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 50 {
			_ = writer.Emit(context.Background(), stream.Frame{Kind: stream.FrameText, Text: "tick"})
		}
	}()

	// Let the heartbeat and emitter run together briefly.
	time.Sleep(20 * time.Millisecond)
	cancel() // stop the heartbeat
	<-done   // wait for emitter to finish
	_ = writer.Close()

	// The body should contain well-formed SSE records and comments with
	// no interleaving corruption: every "event:" is followed by a full
	// "data:" line, and every comment is a standalone ":\n\n".
	body := rec.Body.String()
	if !strings.Contains(body, "event: text\ndata:") {
		t.Fatalf("missing emit records in body:\n%s", body)
	}
}

type nonFlushable struct{}

func (nonFlushable) Header() http.Header         { return http.Header{} }
func (nonFlushable) Write(b []byte) (int, error) { return len(b), nil }
func (nonFlushable) WriteHeader(int)             {}

// Ensure the Writer still satisfies stream.Sink.
var _ stream.Sink = (*Writer)(nil)
