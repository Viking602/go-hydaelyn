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

func TestClientCloseCancelsBlockingInitialize(t *testing.T) {
	// Given
	transport := newBlockingTransport()
	client := New(transport)
	initializeDone := make(chan error, 1)
	go func() {
		_, err := client.Initialize(context.Background(), "test-client", "v1.0.0")
		initializeDone <- err
	}()
	<-transport.started

	// When
	closeDone := make(chan error, 1)
	go func() { closeDone <- client.Close() }()

	// Then
	select {
	case <-transport.closeStarted:
	case <-time.After(time.Second):
		t.Fatal("transport resource close did not start")
	}
	var closeErr error
	returnedEarly := false
	select {
	case closeErr = <-closeDone:
		returnedEarly = true
	case <-time.After(50 * time.Millisecond):
	}
	transport.allowResourceClose()
	if !returnedEarly {
		select {
		case closeErr = <-closeDone:
		case <-time.After(time.Second):
			t.Fatal("Close() did not return after resource cleanup")
		}
	}
	if closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}
	select {
	case err := <-transport.canceled:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Connect context error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close() did not cancel the Initialize context")
	}
	select {
	case err := <-initializeDone:
		if !errors.Is(err, sdkmcp.ErrConnectionClosed) {
			t.Fatalf("Initialize() error = %v, want ErrConnectionClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Initialize() did not return after Close")
	}
	select {
	case <-transport.exited:
	default:
		t.Fatal("Close() returned before Transport.Connect exited")
	}
	select {
	case <-transport.resourceClosed:
	default:
		t.Fatal("Close() returned before the transport resource closed")
	}
	if calls := transport.closeCalls.Load(); calls != 1 {
		t.Fatalf("transport Close() calls = %d, want 1", calls)
	}
	if returnedEarly {
		t.Fatal("Close() returned before the transport resource closed")
	}
}

func TestClientConcurrentInitializeAndCloseAreRaceSafe(t *testing.T) {
	// Given
	client := newInitializedTestClient(t)
	const concurrency = 8
	errorsFound := make(chan error, 2*concurrency)
	var waitGroup sync.WaitGroup
	waitGroup.Add(2 * concurrency)

	// When
	for range concurrency {
		go func() {
			defer waitGroup.Done()
			_, err := client.Initialize(context.Background(), "test-client", "v1.0.0")
			errorsFound <- err
		}()
		go func() {
			defer waitGroup.Done()
			errorsFound <- client.Close()
		}()
	}
	waitGroup.Wait()
	close(errorsFound)

	// Then
	for err := range errorsFound {
		if err != nil && !errors.Is(err, sdkmcp.ErrConnectionClosed) {
			t.Fatalf("concurrent lifecycle error = %v", err)
		}
	}
}

func TestClientConcurrentInitializeSharesOneTransportConnection(t *testing.T) {
	// Given
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	server := newFeatureTestServer()
	serverContext, cancelServer := context.WithCancel(context.Background())
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Run(serverContext, serverTransport) }()
	gated := newGatedTransport(clientTransport)
	client := New(gated)
	t.Cleanup(func() {
		_ = client.Close()
		cancelServer()
		if err := <-serverDone; err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
			t.Errorf("server.Run() error = %v", err)
		}
	})
	const concurrency = 8
	results := make(chan error, concurrency)
	var waitGroup sync.WaitGroup
	waitGroup.Add(concurrency)

	// When
	for range concurrency {
		go func() {
			defer waitGroup.Done()
			_, err := client.Initialize(context.Background(), "test-client", "v1.0.0")
			results <- err
		}()
	}
	<-gated.started
	close(gated.release)
	waitGroup.Wait()
	close(results)

	// Then
	for err := range results {
		if err != nil {
			t.Fatalf("Initialize() error = %v", err)
		}
	}
	if calls := gated.calls.Load(); calls != 1 {
		t.Fatalf("Transport.Connect() calls = %d, want 1", calls)
	}
}

type blockingTransport struct {
	started        chan struct{}
	canceled       chan error
	release        chan struct{}
	exited         chan struct{}
	resourceClosed chan struct{}
	closeStarted   chan struct{}
	allowClose     chan struct{}
	closeCalls     atomic.Int32
	closeOnce      sync.Once
	allowCloseOnce sync.Once
}

func newBlockingTransport() *blockingTransport {
	return &blockingTransport{
		started:        make(chan struct{}),
		canceled:       make(chan error, 1),
		release:        make(chan struct{}),
		exited:         make(chan struct{}),
		resourceClosed: make(chan struct{}),
		closeStarted:   make(chan struct{}),
		allowClose:     make(chan struct{}),
	}
}

func (t *blockingTransport) Connect(ctx context.Context) (sdkmcp.Connection, error) {
	close(t.started)
	defer close(t.exited)
	select {
	case <-ctx.Done():
		t.canceled <- ctx.Err()
		return nil, ctx.Err()
	case <-t.release:
		return nil, errors.New("blocking transport released by test")
	}
}

func (t *blockingTransport) Close() error {
	t.closeOnce.Do(func() {
		close(t.closeStarted)
		<-t.allowClose
		t.closeCalls.Add(1)
		close(t.resourceClosed)
	})
	return nil
}

func (t *blockingTransport) allowResourceClose() {
	t.allowCloseOnce.Do(func() { close(t.allowClose) })
}

type gatedTransport struct {
	delegate sdkmcp.Transport
	started  chan struct{}
	release  chan struct{}
	calls    atomic.Int32
	once     sync.Once
}

func newGatedTransport(delegate sdkmcp.Transport) *gatedTransport {
	return &gatedTransport{
		delegate: delegate,
		started:  make(chan struct{}),
		release:  make(chan struct{}),
	}
}

func (t *gatedTransport) Connect(ctx context.Context) (sdkmcp.Connection, error) {
	t.calls.Add(1)
	t.once.Do(func() { close(t.started) })
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-t.release:
		return t.delegate.Connect(ctx)
	}
}
