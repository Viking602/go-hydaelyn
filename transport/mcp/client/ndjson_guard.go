package mcpclient

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
)

// ErrInvalidFrame identifies an inbound NDJSON line that is not one complete
// JSON value.
var ErrInvalidFrame = errors.New("mcp inbound NDJSON frame is invalid")

// InvalidFrameError reports invalid newline-delimited JSON framing.
type InvalidFrameError struct{}

func (*InvalidFrameError) Error() string { return ErrInvalidFrame.Error() }

func (*InvalidFrameError) Unwrap() error { return ErrInvalidFrame }

type ndjsonLimitReadCloser struct {
	reader   io.ReadCloser
	buffered *bufio.Reader
	limit    int
	pending  []byte
	state    *inboundLimitState
}

func newNDJSONLimitReadCloser(reader io.ReadCloser, limit int, state *inboundLimitState) io.ReadCloser {
	return &ndjsonLimitReadCloser{
		reader:   reader,
		buffered: bufio.NewReader(reader),
		limit:    limit,
		state:    state,
	}
}

func (r *ndjsonLimitReadCloser) Read(payload []byte) (int, error) {
	if err := r.state.Err(); err != nil {
		return 0, err
	}
	if len(payload) == 0 {
		return 0, nil
	}
	if len(r.pending) == 0 {
		frame, err := r.readFrame()
		if err != nil {
			return 0, err
		}
		r.pending = frame
	}
	n := copy(payload, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func (r *ndjsonLimitReadCloser) readFrame() ([]byte, error) {
	frame := make([]byte, 0, min(r.limit+1, 4096))
	for {
		value, err := r.buffered.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) && len(frame) > 0 {
				return nil, r.state.failFrame()
			}
			return nil, err
		}
		if value == '\n' {
			candidate := frame
			if len(candidate) > 0 && candidate[len(candidate)-1] == '\r' {
				candidate = candidate[:len(candidate)-1]
			}
			if len(candidate) > r.limit {
				return nil, r.state.fail(r.limit)
			}
			if len(candidate) == 0 || !json.Valid(candidate) {
				return nil, r.state.failFrame()
			}
			return append(frame, value), nil
		}
		frame = append(frame, value)
		if len(frame) > r.limit && (len(frame) != r.limit+1 || value != '\r') {
			return nil, r.state.fail(r.limit)
		}
	}
}

func (r *ndjsonLimitReadCloser) Close() error { return r.reader.Close() }
