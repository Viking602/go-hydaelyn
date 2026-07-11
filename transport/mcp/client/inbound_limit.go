package mcpclient

import (
	"bufio"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
)

const maxInboundMessageBytes = 4 << 20

// InboundLimitError reports an inbound MCP payload that exceeded the byte limit.
type InboundLimitError struct {
	Limit int
}

func (e *InboundLimitError) Error() string {
	return fmt.Sprintf("mcp inbound payload exceeds %d bytes", e.Limit)
}

type inboundLimitState struct {
	mu  sync.Mutex
	err error
}

func newInboundLimitState() *inboundLimitState {
	return &inboundLimitState{}
}

func (s *inboundLimitState) fail(limit int) error {
	return s.stick(&InboundLimitError{Limit: limit})
}

func (s *inboundLimitState) failFrame() error {
	return s.stick(&InvalidFrameError{})
}

func (s *inboundLimitState) stick(err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
	return s.err
}

func (s *inboundLimitState) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		return nil
	}
	return s.err
}

type inboundLimitSource interface {
	inboundLimitError() error
}

func (c *Client) inboundLimitError() error {
	source, ok := c.transport.(inboundLimitSource)
	if !ok {
		return nil
	}
	return source.inboundLimitError()
}

func (c *Client) operationError(err error) error {
	if limitErr := c.inboundLimitError(); limitErr != nil {
		return limitErr
	}
	return adaptRPCError(err)
}

func limitHTTPResponse(response *http.Response, state *inboundLimitState) {
	if response == nil || response.Body == nil {
		return
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil {
		return
	}
	switch {
	case mediaType == "text/event-stream":
		response.Body = newSSELimitReadCloser(response.Body, maxInboundMessageBytes, state)
	case mediaType == "application/json" || strings.HasSuffix(mediaType, "+json"):
		response.Body = newBodyLimitReadCloser(response.Body, maxInboundMessageBytes, state)
	}
}

type sseLimitReadCloser struct {
	reader     io.ReadCloser
	buffered   *bufio.Reader
	limit      int
	eventBytes int
	lineBytes  int
	state      *inboundLimitState
}

func newSSELimitReadCloser(reader io.ReadCloser, limit int, state *inboundLimitState) io.ReadCloser {
	return &sseLimitReadCloser{
		reader:   reader,
		buffered: bufio.NewReader(reader),
		limit:    limit,
		state:    state,
	}
}

func (r *sseLimitReadCloser) Read(payload []byte) (int, error) {
	if err := r.state.Err(); err != nil {
		return 0, err
	}
	written := 0
	for written < len(payload) {
		value, err := r.buffered.ReadByte()
		if err != nil {
			if written > 0 {
				return written, nil
			}
			return 0, err
		}
		if value == '\r' && r.lineBytes == 0 {
			next, peekErr := r.buffered.Peek(1)
			if peekErr == nil && next[0] == '\n' {
				payload[written] = value
				written++
				continue
			}
		}
		if value == '\n' {
			if r.lineBytes == 0 {
				r.eventBytes = 0
			}
			r.lineBytes = 0
		} else {
			if r.eventBytes == r.limit {
				err := r.state.fail(r.limit)
				if written == 0 {
					return 0, err
				}
				return written, nil
			}
			r.eventBytes++
			r.lineBytes++
		}
		payload[written] = value
		written++
	}
	return written, nil
}

func (r *sseLimitReadCloser) Close() error { return r.reader.Close() }

type bodyLimitReadCloser struct {
	reader io.ReadCloser
	limit  int
	read   int
	state  *inboundLimitState
}

func newBodyLimitReadCloser(reader io.ReadCloser, limit int, state *inboundLimitState) io.ReadCloser {
	return &bodyLimitReadCloser{reader: reader, limit: limit, state: state}
}

func (r *bodyLimitReadCloser) Read(payload []byte) (int, error) {
	if err := r.state.Err(); err != nil {
		return 0, err
	}
	if len(payload) == 0 {
		return 0, nil
	}
	remaining := r.limit - r.read
	readSize := min(len(payload), remaining+1)
	n, readErr := r.reader.Read(payload[:readSize])
	if n <= remaining {
		r.read += n
		return n, readErr
	}
	r.read += remaining
	err := r.state.fail(r.limit)
	if remaining == 0 {
		return 0, err
	}
	return remaining, nil
}

func (r *bodyLimitReadCloser) Close() error { return r.reader.Close() }
