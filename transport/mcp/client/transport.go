package mcpclient

import (
	"context"
	"errors"
	"io"
	"reflect"
	"sync"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Transport is the official MCP transport contract.
type Transport = sdkmcp.Transport

type StreamTransport struct {
	delegate   *sdkmcp.IOTransport
	resource   *streamResource
	limitState *inboundLimitState
}

func NewStreamTransport(reader io.Reader, writer io.Writer, closers ...io.Closer) *StreamTransport {
	resourceClosers := make([]io.Closer, 0, len(closers)+2)
	if len(closers) == 0 {
		if closer, ok := reader.(io.Closer); ok {
			resourceClosers = appendUniqueCloser(resourceClosers, closer)
		}
		if closer, ok := writer.(io.Closer); ok {
			resourceClosers = appendUniqueCloser(resourceClosers, closer)
		}
	} else {
		for _, closer := range closers {
			resourceClosers = appendUniqueCloser(resourceClosers, closer)
		}
	}
	resource := &streamResource{reader: reader, writer: writer, closers: resourceClosers}
	limitState := newInboundLimitState()
	return &StreamTransport{
		resource:   resource,
		limitState: limitState,
		delegate: &sdkmcp.IOTransport{
			Reader: newNDJSONLimitReadCloser(streamReader{resource}, maxInboundMessageBytes, limitState),
			Writer: streamWriter{resource},
		},
	}
}

func appendUniqueCloser(closers []io.Closer, candidate io.Closer) []io.Closer {
	if isNilLike(candidate) {
		return closers
	}
	for _, closer := range closers {
		if sameCloser(closer, candidate) {
			return closers
		}
	}
	return append(closers, candidate)
}

func sameCloser(left, right io.Closer) bool {
	leftType := reflect.TypeOf(left)
	if leftType != reflect.TypeOf(right) {
		return false
	}
	if leftType.Comparable() {
		return left == right
	}
	leftValue := reflect.ValueOf(left)
	rightValue := reflect.ValueOf(right)
	switch leftValue.Kind() {
	case reflect.Map, reflect.Slice:
		return leftValue.Pointer() == rightValue.Pointer()
	default:
		return false
	}
}

func isNilLike(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func (t *StreamTransport) Connect(ctx context.Context) (sdkmcp.Connection, error) {
	return t.delegate.Connect(ctx)
}

func (t *StreamTransport) Close() error { return t.resource.Close() }

func (t *StreamTransport) inboundLimitError() error { return t.limitState.Err() }

type streamResource struct {
	reader    io.Reader
	writer    io.Writer
	closers   []io.Closer
	closeOnce sync.Once
	closeErr  error
}

func (r *streamResource) Close() error {
	r.closeOnce.Do(func() {
		for _, closer := range r.closers {
			if closer != nil {
				r.closeErr = errors.Join(r.closeErr, closer.Close())
			}
		}
	})
	return r.closeErr
}

type streamReader struct{ *streamResource }

func (r streamReader) Read(payload []byte) (int, error) { return r.reader.Read(payload) }

type streamWriter struct{ *streamResource }

func (w streamWriter) Write(payload []byte) (int, error) { return w.writer.Write(payload) }

type trackedTransport struct {
	delegate  sdkmcp.Transport
	mu        sync.Mutex
	conn      sdkmcp.Connection
	closeOnce sync.Once
	closeErr  error
}

func (t *trackedTransport) Connect(ctx context.Context) (sdkmcp.Connection, error) {
	conn, err := t.delegate.Connect(ctx)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.conn = conn
	t.mu.Unlock()
	return conn, nil
}

func (t *trackedTransport) Close() error {
	t.closeOnce.Do(func() {
		t.mu.Lock()
		conn := t.conn
		t.mu.Unlock()
		if conn != nil {
			t.closeErr = conn.Close()
		} else if closer, ok := t.delegate.(interface{ Close() error }); ok {
			t.closeErr = closer.Close()
		}
	})
	return t.closeErr
}
