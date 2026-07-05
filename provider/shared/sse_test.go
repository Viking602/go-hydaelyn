package shared

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestReaderParsesBasicEvent(t *testing.T) {
	body := strings.NewReader("event: message\ndata: hello\n\n")
	reader := NewReader(body)

	evt, err := reader.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Name != "message" {
		t.Errorf("expected name 'message', got %q", evt.Name)
	}
	if evt.Data != "hello" {
		t.Errorf("expected data 'hello', got %q", evt.Data)
	}
	if evt.ID != "" {
		t.Errorf("expected empty id, got %q", evt.ID)
	}

	_, err = reader.Next()
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestReaderParsesMultiLineData(t *testing.T) {
	body := strings.NewReader("data: hello\ndata: world\n\n")
	reader := NewReader(body)

	evt, err := reader.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Data != "hello\nworld" {
		t.Errorf("expected multi-line data, got %q", evt.Data)
	}
}

func TestReaderParsesID(t *testing.T) {
	body := strings.NewReader("id: 123\nevent: update\ndata: payload\n\n")
	reader := NewReader(body)

	evt, err := reader.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.ID != "123" {
		t.Errorf("expected id '123', got %q", evt.ID)
	}
	if evt.Name != "update" {
		t.Errorf("expected name 'update', got %q", evt.Name)
	}
	if evt.Data != "payload" {
		t.Errorf("expected data 'payload', got %q", evt.Data)
	}
}

func TestReaderHandlesComments(t *testing.T) {
	body := strings.NewReader(":keepalive\n\n")
	reader := NewReader(body)

	evt, err := reader.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Comment != "keepalive" {
		t.Errorf("expected comment 'keepalive', got %q", evt.Comment)
	}
	if evt.Data != "" {
		t.Errorf("expected empty data for comment-only frame, got %q", evt.Data)
	}
}

func TestReaderSkipsEmptyFrames(t *testing.T) {
	body := strings.NewReader("\n\ndata: after-empty\n\n")
	reader := NewReader(body)

	evt, err := reader.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Data != "after-empty" {
		t.Errorf("expected data 'after-empty', got %q", evt.Data)
	}
}

func TestReaderHandlesLeadingSpaceAfterColon(t *testing.T) {
	body := strings.NewReader("data:  hello\nevent:  ping\n\n")
	reader := NewReader(body)

	evt, err := reader.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Data != " hello" {
		t.Errorf("expected data ' hello', got %q", evt.Data)
	}
	if evt.Name != " ping" {
		t.Errorf("expected name ' ping', got %q", evt.Name)
	}
}

func TestReaderHandlesNoSpaceAfterColon(t *testing.T) {
	body := strings.NewReader("data:value\n\n")
	reader := NewReader(body)

	evt, err := reader.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Data != "value" {
		t.Errorf("expected data 'value', got %q", evt.Data)
	}
}

// TestReaderHandlesTrailingFrameWithoutDoubleNewline asserts the
// post-fix contract: a frame without a terminating blank line is
// truncated, not a complete event. A network drop mid-frame must
// surface as io.ErrUnexpectedEOF so the provider Recv path reports a
// transport fault rather than silently accepting a partial payload.
func TestReaderHandlesTrailingFrameWithoutDoubleNewline(t *testing.T) {
	body := strings.NewReader("data: trailing")
	reader := NewReader(body)

	_, err := reader.Next()
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected io.ErrUnexpectedEOF for truncated frame, got %v", err)
	}
}

// TestReaderTruncatedMidJSONReturnsErrUnexpectedEOF is the regression for
// the silent-truncation bug: a connection drop mid-frame used to return
// the partial data as a valid Event, so a provider feeding it to
// json.Unmarshal could parse a permissive struct to a wrong result. The
// fix surfaces a mid-frame EOF as io.ErrUnexpectedEOF.
func TestReaderTruncatedMidJSONReturnsErrUnexpectedEOF(t *testing.T) {
	// A valid frame would end with a blank line; this one is cut mid-data.
	body := strings.NewReader("event: message\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"hel")
	reader := NewReader(body)

	_, err := reader.Next()
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected io.ErrUnexpectedEOF for mid-frame truncation, got %v", err)
	}
}
