package mcpclient

import (
	"context"
	"io"
	"sync/atomic"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNewStreamTransportConnectsOfficialIOSession(t *testing.T) {
	// Given
	serverReader, clientWriter := io.Pipe()
	clientReader, serverWriter := io.Pipe()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "io-server", Version: "v1.0.0"}, nil)
	server.AddTool(&sdkmcp.Tool{Name: "ready", InputSchema: map[string]any{"type": "object"}}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run(ctx, &sdkmcp.IOTransport{Reader: serverReader, Writer: serverWriter})
	}()
	client := New(NewStreamTransport(clientReader, clientWriter, clientReader, clientWriter))
	t.Cleanup(func() {
		_ = client.Close()
		cancel()
		<-serverDone
	})

	// When
	_, err := client.Initialize(context.Background(), "io-client", "v1.0.0")
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	tools, err := client.ListTools(context.Background())

	// Then
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "ready" {
		t.Fatalf("ListTools() = %#v", tools)
	}
}

func TestNewStreamTransportClosesSharedReadWriteCloserOnce(t *testing.T) {
	// Given
	stream := &countingReadWriteCloser{}
	transport := NewStreamTransport(stream, stream)

	// When
	err := transport.Close()

	// Then
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if count := stream.closeCount.Load(); count != 1 {
		t.Fatalf("Close() count = %d, want 1", count)
	}
}

func TestNewStreamTransportClosesDuplicateExplicitCloserOnce(t *testing.T) {
	// Given
	stream := &countingReadWriteCloser{}
	transport := NewStreamTransport(stream, stream, stream, stream)

	// When
	err := transport.Close()

	// Then
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if count := stream.closeCount.Load(); count != 1 {
		t.Fatalf("Close() count = %d, want 1", count)
	}
}

func TestNewStreamTransportDeduplicatesUncomparableCloserWithoutPanic(t *testing.T) {
	// Given
	var closeCount atomic.Int32
	closer := uncomparableCloser{func() { closeCount.Add(1) }}
	transport := NewStreamTransport(&countingReadWriteCloser{}, io.Discard, closer, closer)

	// When
	err := transport.Close()

	// Then
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if count := closeCount.Load(); count != 1 {
		t.Fatalf("Close() count = %d, want 1", count)
	}
}

type countingReadWriteCloser struct {
	closeCount atomic.Int32
}

func (*countingReadWriteCloser) Read([]byte) (int, error) { return 0, io.EOF }

func (*countingReadWriteCloser) Write(payload []byte) (int, error) { return len(payload), nil }

func (c *countingReadWriteCloser) Close() error {
	c.closeCount.Add(1)
	return nil
}

type uncomparableCloser []func()

func (c uncomparableCloser) Close() error {
	for _, closeFunc := range c {
		closeFunc()
	}
	return nil
}
