package mcpclient

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const testResponseHeaderTimeout = 50 * time.Millisecond

func TestHTTPTransportTimesOutWaitingForResponseHeaders(t *testing.T) {
	// Given
	handlerStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(handlerStarted)
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)
	transport := NewHTTPTransport(server.URL, nil)
	configureResponseHeaderTimeout(t, transport, testResponseHeaderTimeout)

	// When
	_, err := transport.client.Get(server.URL)

	// Then
	<-handlerStarted
	var networkError net.Error
	if !errors.As(err, &networkError) || !networkError.Timeout() {
		t.Fatalf("Get() error = %T %v, want response header timeout", err, err)
	}
}

func TestHTTPTransportDoesNotApplyResponseHeaderTimeoutToIdleSSEBody(t *testing.T) {
	// Given
	headersFlushed := make(chan struct{})
	releaseBody := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		writer.(http.Flusher).Flush()
		close(headersFlushed)
		<-releaseBody
		_, _ = io.WriteString(writer, "data: ready\n\n")
	}))
	t.Cleanup(server.Close)
	transport := NewHTTPTransport(server.URL, nil)
	configureResponseHeaderTimeout(t, transport, testResponseHeaderTimeout)

	// When
	response, err := transport.client.Get(server.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	<-headersFlushed
	timer := time.NewTimer(2 * testResponseHeaderTimeout)
	defer timer.Stop()
	<-timer.C
	close(releaseBody)
	payload, err := io.ReadAll(response.Body)

	// Then
	if err != nil {
		t.Fatalf("ReadAll() after idle SSE period error = %v", err)
	}
	if string(payload) != "data: ready\n\n" {
		t.Fatalf("SSE payload = %q, want ready event", payload)
	}
}

func configureResponseHeaderTimeout(t *testing.T, transport *HTTPTransport, timeout time.Duration) {
	t.Helper()
	roundTripper := transport.client.Transport.(*headerRoundTripper)
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.ResponseHeaderTimeout = timeout
	roundTripper.base = base
}
