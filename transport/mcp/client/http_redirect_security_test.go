package mcpclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

var originBoundTestHeaders = http.Header{
	"Authorization":        {"Bearer secret"},
	"Cookie":               {"session=secret"},
	"Proxy-Authorization":  {"Basic proxy-secret"},
	"Mcp-Session-Id":       {"hydaelyn-session"},
	"Mcp-Protocol-Version": {"2025-06-18"},
	"Last-Event-Id":        {"event-42"},
}

func TestHTTPTransportRejectsCrossOrigin307And308Redirects(t *testing.T) {
	// Given
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		targetCalls.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(target.Close)
	featureServer := newHTTPFeatureServer()
	officialHandler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server {
		return featureServer
	}, &sdkmcp.StreamableHTTPOptions{JSONResponse: true})
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/redirect-307":
			http.Redirect(writer, request, target.URL, http.StatusTemporaryRedirect)
		case "/redirect-308":
			http.Redirect(writer, request, target.URL, http.StatusPermanentRedirect)
		default:
			officialHandler.ServeHTTP(writer, request)
		}
	}))
	t.Cleanup(source.Close)
	transport := NewHTTPTransport(source.URL, nil)
	client := New(transport)
	t.Cleanup(func() { _ = client.Close() })
	initialized, err := client.Initialize(context.Background(), "http-client", "v1.0.0")
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	// When
	for _, path := range []string{"/redirect-307", "/redirect-308"} {
		request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, source.URL+path, strings.NewReader("{}"))
		if err != nil {
			t.Fatalf("NewRequest() error = %v", err)
		}
		for name, values := range originBoundTestHeaders {
			request.Header[name] = append([]string(nil), values...)
		}
		request.Header.Set("Mcp-Protocol-Version", initialized.ProtocolVersion)
		originalHeaders := request.Header.Clone()
		response, err := transport.client.Do(request)
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		if !errors.Is(err, errCrossOriginRedirect) {
			t.Fatalf("Do(%s) error = %v, want cross-origin rejection", path, err)
		}
		if !reflect.DeepEqual(request.Header, originalHeaders) {
			t.Fatalf("Do(%s) mutated original headers: got %#v want %#v", path, request.Header, originalHeaders)
		}
	}

	// Then
	if calls := targetCalls.Load(); calls != 0 {
		t.Fatalf("cross-origin redirect target calls = %d, want 0", calls)
	}
}

func TestHTTPTransportPreservesOriginBoundHeadersAcrossSameOriginRedirect(t *testing.T) {
	// Given
	observed := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/redirect" {
			http.Redirect(writer, request, "/target", http.StatusPermanentRedirect)
			return
		}
		observed <- request.Header.Clone()
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	transport := NewHTTPTransport(server.URL, nil)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL+"/redirect", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	for name, values := range originBoundTestHeaders {
		request.Header[name] = append([]string(nil), values...)
	}
	originalHeaders := request.Header.Clone()

	// When
	response, err := transport.client.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	_ = response.Body.Close()
	headers := <-observed

	// Then
	for name, values := range originBoundTestHeaders {
		if got := headers.Values(name); len(got) != len(values) || got[0] != values[0] {
			t.Fatalf("same-origin redirect header %s = %#v, want %#v", name, got, values)
		}
	}
	if !reflect.DeepEqual(request.Header, originalHeaders) {
		t.Fatalf("Do() mutated original headers: got %#v want %#v", request.Header, originalHeaders)
	}
}
