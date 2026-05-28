package sse

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/stream"
)

func TestWriterEmitsServerSentEvents(t *testing.T) {
	rec := httptest.NewRecorder()
	writer, err := NewWriter(rec)
	if err != nil {
		t.Fatalf("NewWriter error = %v", err)
	}
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
	if _, err := NewWriter(nonFlushable{}); !errors.Is(err, ErrNotFlushable) {
		t.Fatalf("NewWriter error = %v, want ErrNotFlushable", err)
	}
}

type nonFlushable struct{}

func (nonFlushable) Header() http.Header         { return http.Header{} }
func (nonFlushable) Write(b []byte) (int, error) { return len(b), nil }
func (nonFlushable) WriteHeader(int)             {}
