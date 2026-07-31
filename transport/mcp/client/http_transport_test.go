package mcpclient

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHTTPTransportPreservesSDKOwnedHeadersWhenCustomHeadersConflict(t *testing.T) {
	// Given
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "http-server", Version: "v1.0.0"}, nil)
	httpServer := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server {
		return server
	}, &sdkmcp.StreamableHTTPOptions{JSONResponse: true}))
	t.Cleanup(httpServer.Close)
	transport := NewHTTPTransport(httpServer.URL, http.Header{
		"Accept":               {"text/plain"},
		"Content-Type":         {"text/plain"},
		"Mcp-Session-Id":       {"spoofed-session"},
		"Mcp-Protocol-Version": {"1900-01-01"},
		"X-Venat-Test":         {"preserved"},
	})
	client := New(transport)
	t.Cleanup(func() { _ = client.Close() })

	// When
	_, err := client.Initialize(context.Background(), "http-client", "v1.0.0")
	// Then
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
}

func TestHeaderRoundTripperClosesDeleteResponseBodyAfterStickyOverflow(t *testing.T) {
	// Given
	state := newInboundLimitState()
	state.fail(1)
	body := &trackingHTTPBody{}
	requests := 0
	roundTripper := &headerRoundTripper{
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}, nil
		}),
		limitState: state,
	}
	request := httptest.NewRequest(http.MethodDelete, "http://example.test/mcp", nil)

	// When
	response, err := roundTripper.RoundTrip(request)
	// Then
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("DELETE requests = %d, want 1", requests)
	}
	if body.closeCount != 1 {
		t.Fatalf("response body Close() calls = %d, want 1", body.closeCount)
	}
	if response.Body != http.NoBody {
		t.Fatalf("response body = %T, want http.NoBody", response.Body)
	}
}

func TestHeaderRoundTripperReturnsDeleteResponseBodyCloseError(t *testing.T) {
	// Given
	wantErr := errors.New("close response body")
	body := &trackingHTTPBody{closeErr: wantErr}
	roundTripper := &headerRoundTripper{
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}, nil
		}),
		limitState: newInboundLimitState(),
	}
	request := httptest.NewRequest(http.MethodDelete, "http://example.test/mcp", nil)

	// When
	_, err := roundTripper.RoundTrip(request)

	// Then
	if !errors.Is(err, wantErr) {
		t.Fatalf("RoundTrip() error = %v, want %v", err, wantErr)
	}
	if body.closeCount != 1 {
		t.Fatalf("response body Close() calls = %d, want 1", body.closeCount)
	}
}

func TestHTTPTransportCloseClosesPrivateIdleConnections(t *testing.T) {
	// Given
	idle := make(chan struct{}, 1)
	closed := make(chan struct{}, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Length", "2")
		_, _ = writer.Write([]byte("ok"))
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		switch state {
		case http.StateIdle:
			select {
			case idle <- struct{}{}:
			default:
			}
		case http.StateClosed:
			select {
			case closed <- struct{}{}:
			default:
			}
		}
	}
	server.Start()
	t.Cleanup(server.Close)
	transport := NewHTTPTransport(server.URL, nil)
	response, err := transport.client.Get(server.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	select {
	case <-idle:
	case <-time.After(time.Second):
		t.Fatal("connection did not become idle")
	}

	// When
	if err := transport.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Then
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close() did not close the private idle connection")
	}
}

type trackingHTTPBody struct {
	closeCount int
	closeErr   error
}

func (*trackingHTTPBody) Read([]byte) (int, error) { return 0, io.EOF }

func (b *trackingHTTPBody) Close() error {
	b.closeCount++
	return b.closeErr
}
