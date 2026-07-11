package mcpclient

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	errTerminalConnect = errors.New("terminal connect failure")
	errTerminalClose   = errors.New("terminal transport close failure")
	errSessionClose    = errors.New("session close failure")
)

func TestClientSequentialInitializeCachesTerminalFailure(t *testing.T) {
	// Given
	transport := newGatedFailingTransport()
	close(transport.release)
	client := New(transport)

	// When
	_, firstErr := client.Initialize(context.Background(), "first", "v1.0.0")
	_, secondErr := client.Initialize(context.Background(), "second", "v2.0.0")
	closeErr := client.Close()

	// Then
	assertSameTerminalInitializeError(t, firstErr, secondErr)
	if !errors.Is(closeErr, errTerminalClose) {
		t.Fatalf("Close() error = %v, want terminal close error", closeErr)
	}
	if calls := transport.connectCalls.Load(); calls != 1 {
		t.Fatalf("Transport.Connect() calls = %d, want 1", calls)
	}
	if calls := transport.closeCalls.Load(); calls != 1 {
		t.Fatalf("Transport.Close() calls = %d, want 1", calls)
	}
}

func TestClientConcurrentInitializeCachesTerminalFailure(t *testing.T) {
	// Given
	transport := newGatedFailingTransport()
	client := New(transport)
	firstDone := make(chan error, 1)
	waiterDone := make(chan error, 1)
	go func() {
		_, err := client.Initialize(context.Background(), "first", "v1.0.0")
		firstDone <- err
	}()
	<-transport.started
	go func() {
		_, err := client.Initialize(context.Background(), "waiter", "v1.0.0")
		waiterDone <- err
	}()
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	_, canceledErr := client.Initialize(canceledContext, "canceled", "v1.0.0")

	// When
	close(transport.release)
	firstErr := <-firstDone
	waiterErr := <-waiterDone
	_, laterErr := client.Initialize(context.Background(), "later", "v1.0.0")

	// Then
	if !errors.Is(canceledErr, context.Canceled) {
		t.Fatalf("waiting Initialize() error = %v, want context.Canceled", canceledErr)
	}
	assertSameTerminalInitializeError(t, firstErr, waiterErr)
	assertSameTerminalInitializeError(t, firstErr, laterErr)
	if calls := transport.connectCalls.Load(); calls != 1 {
		t.Fatalf("Transport.Connect() calls = %d, want 1", calls)
	}
}

func TestClientConcurrentCloseReturnsSameSessionError(t *testing.T) {
	// Given
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	server := newFeatureTestServer()
	serverContext, cancelServer := context.WithCancel(context.Background())
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Run(serverContext, serverTransport) }()
	transport := newControlledCloseTransport(clientTransport)
	client := New(transport)
	if _, err := client.Initialize(context.Background(), "test-client", "v1.0.0"); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	t.Cleanup(func() {
		cancelServer()
		if err := <-serverDone; err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
			t.Errorf("server.Run() error = %v", err)
		}
	})
	const concurrency = 8
	start := make(chan struct{})
	ready := make(chan struct{}, concurrency)
	results := make(chan error, concurrency)
	for range concurrency {
		go func() {
			ready <- struct{}{}
			<-start
			results <- client.Close()
		}()
	}
	for range concurrency {
		<-ready
	}
	close(start)
	<-transport.connection.closeStarted

	// When
	select {
	case err := <-results:
		close(transport.connection.releaseClose)
		t.Fatalf("Close() returned before close owner completed: %v", err)
	case <-time.After(250 * time.Millisecond):
		close(transport.connection.releaseClose)
	}

	// Then
	for range concurrency {
		err := <-results
		if !errors.Is(err, errSessionClose) {
			t.Fatalf("Close() error = %v, want session close error", err)
		}
		if err.Error() != errSessionClose.Error() {
			t.Fatalf("Close() error string = %q, want %q", err, errSessionClose)
		}
	}
	if calls := transport.connection.closeCalls.Load(); calls != 1 {
		t.Fatalf("session resource Close() calls = %d, want 1", calls)
	}
}

func assertSameTerminalInitializeError(t *testing.T, left, right error) {
	t.Helper()
	for _, err := range []error{left, right} {
		if !errors.Is(err, errTerminalConnect) || !errors.Is(err, errTerminalClose) {
			t.Fatalf("Initialize() error chain = %v, want connect and close errors", err)
		}
	}
	if left.Error() != right.Error() {
		t.Fatalf("Initialize() errors differ: left=%q right=%q", left, right)
	}
}

type gatedFailingTransport struct {
	started      chan struct{}
	release      chan struct{}
	startOnce    sync.Once
	connectCalls atomic.Int32
	closeCalls   atomic.Int32
}

func newGatedFailingTransport() *gatedFailingTransport {
	return &gatedFailingTransport{started: make(chan struct{}), release: make(chan struct{})}
}

func (t *gatedFailingTransport) Connect(context.Context) (sdkmcp.Connection, error) {
	t.connectCalls.Add(1)
	t.startOnce.Do(func() { close(t.started) })
	<-t.release
	return nil, errTerminalConnect
}

func (t *gatedFailingTransport) Close() error {
	t.closeCalls.Add(1)
	return errTerminalClose
}

type controlledCloseTransport struct {
	delegate   sdkmcp.Transport
	connection *controlledCloseConnection
}

func newControlledCloseTransport(delegate sdkmcp.Transport) *controlledCloseTransport {
	return &controlledCloseTransport{delegate: delegate}
}

func (t *controlledCloseTransport) Connect(ctx context.Context) (sdkmcp.Connection, error) {
	connection, err := t.delegate.Connect(ctx)
	if err != nil {
		return nil, err
	}
	t.connection = &controlledCloseConnection{
		Connection:   connection,
		closeStarted: make(chan struct{}),
		releaseClose: make(chan struct{}),
	}
	return t.connection, nil
}

type controlledCloseConnection struct {
	sdkmcp.Connection
	closeStarted chan struct{}
	releaseClose chan struct{}
	closeOnce    sync.Once
	closeCalls   atomic.Int32
}

func (c *controlledCloseConnection) Close() error {
	c.closeOnce.Do(func() {
		close(c.closeStarted)
		<-c.releaseClose
		c.closeCalls.Add(1)
		_ = c.Connection.Close()
	})
	return errSessionClose
}
