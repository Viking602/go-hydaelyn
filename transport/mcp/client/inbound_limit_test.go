package mcpclient

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestNDJSONLimitReaderAcceptsExactLimitAcrossChunksAndCRLF(t *testing.T) {
	// Given
	state := newInboundLimitState()
	reader := newNDJSONLimitReadCloser(&chunkedReadCloser{
		reader: strings.NewReader("1234\n123\r\n"),
		max:    1,
	}, 4, state)

	// When
	payload, err := io.ReadAll(reader)

	// Then
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(payload) != "1234\n123\r\n" {
		t.Fatalf("ReadAll() = %q", payload)
	}
}

func TestNDJSONLimitReaderRejectsLimitPlusOneWithStickyError(t *testing.T) {
	// Given
	state := newInboundLimitState()
	reader := newNDJSONLimitReadCloser(io.NopCloser(strings.NewReader("12345\n")), 4, state)

	// When
	payload, err := io.ReadAll(reader)
	_, repeatedErr := reader.Read(make([]byte, 1))

	// Then
	if len(payload) != 0 {
		t.Fatalf("payload exposed before validation = %q", payload)
	}
	assertStickyInboundLimitError(t, err, repeatedErr, 4)
}

func TestNDJSONLimitReaderRejectsMultilineJSONBeforeExposure(t *testing.T) {
	// Given
	state := newInboundLimitState()
	reader := newNDJSONLimitReadCloser(io.NopCloser(strings.NewReader("{\n  \"value\": 1\n}\n")), 64, state)

	// When
	payload, err := io.ReadAll(reader)
	_, repeatedErr := reader.Read(make([]byte, 1))

	// Then
	if len(payload) != 0 {
		t.Fatalf("payload exposed before validation = %q", payload)
	}
	assertStickyInvalidFrameError(t, err, repeatedErr)
}

func TestNDJSONLimitReaderAcceptsCompleteSingleLineFrames(t *testing.T) {
	tests := map[string]string{
		"escaped newline": "{\"text\":\"line\\nnext\"}\n",
		"CRLF":            "{\"value\":1}\r\n",
		"multiple":        "{}\n[]\n1\n",
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			state := newInboundLimitState()
			reader := newNDJSONLimitReadCloser(&chunkedReadCloser{reader: strings.NewReader(payload), max: 1}, len(payload), state)

			got, err := io.ReadAll(reader)

			if err != nil {
				t.Fatalf("ReadAll() error = %v", err)
			}
			if string(got) != payload {
				t.Fatalf("ReadAll() = %q, want %q", got, payload)
			}
		})
	}
}

func TestNDJSONLimitReaderRejectsInvalidEmptyAndIncompleteFrames(t *testing.T) {
	tests := map[string]string{
		"invalid":    "not-json\n",
		"empty":      "\n",
		"incomplete": "{}",
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			state := newInboundLimitState()
			reader := newNDJSONLimitReadCloser(io.NopCloser(strings.NewReader(payload)), 64, state)

			got, err := io.ReadAll(reader)
			_, repeatedErr := reader.Read(make([]byte, 1))

			if len(got) != 0 {
				t.Fatalf("payload exposed before validation = %q", got)
			}
			assertStickyInvalidFrameError(t, err, repeatedErr)
		})
	}
}

func TestSSELimitReaderAcceptsMultipleEventsWhoseTotalExceedsLimit(t *testing.T) {
	// Given
	const eventLimit = 7
	payload := "data:12\n\ndata:12\n\n"
	state := newInboundLimitState()
	reader := newSSELimitReadCloser(&chunkedReadCloser{reader: strings.NewReader(payload), max: 2}, eventLimit, state)

	// When
	got, err := io.ReadAll(reader)

	// Then
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(got) != payload {
		t.Fatalf("ReadAll() = %q, want %q", got, payload)
	}
}

func TestSSELimitReaderHandlesCRLFEventBoundaries(t *testing.T) {
	// Given
	payload := "data:x\r\n\r\ndata:y\r\n\r\n"
	state := newInboundLimitState()
	reader := newSSELimitReadCloser(io.NopCloser(strings.NewReader(payload)), 7, state)

	// When
	got, err := io.ReadAll(reader)

	// Then
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(got) != payload {
		t.Fatalf("ReadAll() = %q, want %q", got, payload)
	}
}

func TestSSELimitReaderRejectsSingleEventLimitPlusOneWithStickyError(t *testing.T) {
	// Given
	state := newInboundLimitState()
	reader := newSSELimitReadCloser(io.NopCloser(strings.NewReader("data:123\n\n")), 7, state)

	// When
	payload, err := io.ReadAll(reader)
	_, repeatedErr := reader.Read(make([]byte, 1))

	// Then
	if string(payload) != "data:12" {
		t.Fatalf("payload before overflow = %q, want exact limit", payload)
	}
	assertStickyInboundLimitError(t, err, repeatedErr, 7)
}

func TestBodyLimitReaderAcceptsExactLimitAcrossChunks(t *testing.T) {
	// Given
	state := newInboundLimitState()
	reader := newBodyLimitReadCloser(&chunkedReadCloser{reader: strings.NewReader("1234"), max: 1}, 4, state)

	// When
	payload, err := io.ReadAll(reader)

	// Then
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(payload) != "1234" {
		t.Fatalf("ReadAll() = %q, want exact limit", payload)
	}
}

func TestBodyLimitReaderRejectsLimitPlusOneWithStickyError(t *testing.T) {
	// Given
	state := newInboundLimitState()
	reader := newBodyLimitReadCloser(io.NopCloser(strings.NewReader("12345")), 4, state)

	// When
	payload, err := io.ReadAll(reader)
	_, repeatedErr := reader.Read(make([]byte, 1))

	// Then
	if string(payload) != "1234" {
		t.Fatalf("payload before overflow = %q, want exact limit", payload)
	}
	assertStickyInboundLimitError(t, err, repeatedErr, 4)
}

func assertStickyInboundLimitError(t *testing.T, first, second error, limit int) {
	t.Helper()
	var limitErr *InboundLimitError
	if !errors.As(first, &limitErr) || limitErr.Limit != limit {
		t.Fatalf("first error = %T %v, want InboundLimitError(%d)", first, first, limit)
	}
	if first != second {
		t.Fatalf("sticky errors differ: first=%p second=%p", first, second)
	}
}

func assertStickyInvalidFrameError(t *testing.T, first, second error) {
	t.Helper()
	var frameErr *InvalidFrameError
	if !errors.Is(first, ErrInvalidFrame) || !errors.As(first, &frameErr) {
		t.Fatalf("first error = %T %v, want InvalidFrameError", first, first)
	}
	if first != second {
		t.Fatalf("sticky errors differ: first=%p second=%p", first, second)
	}
}

type chunkedReadCloser struct {
	reader io.Reader
	max    int
}

func (r *chunkedReadCloser) Read(payload []byte) (int, error) {
	if len(payload) > r.max {
		payload = payload[:r.max]
	}
	return r.reader.Read(payload)
}

func (*chunkedReadCloser) Close() error { return nil }
