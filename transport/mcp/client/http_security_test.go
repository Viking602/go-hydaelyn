package mcpclient

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

var sensitiveTestHeaders = http.Header{
	"Authorization": {"Bearer secret"},
	"Cookie":        {"session=secret"},
	"X-Api-Key":     {"api-secret"},
	"X-Custom":      {"same-origin-only"},
}

func TestHTTPTransportDoesNotInjectCustomHeadersAcrossOriginRedirect(t *testing.T) {
	// Given
	observed := make(chan http.Header, 1)
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		observed <- request.Header.Clone()
	}))
	t.Cleanup(target.Close)
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	t.Cleanup(source.Close)
	transport := NewHTTPTransport(source.URL, sensitiveTestHeaders)

	// When
	response, err := transport.client.Get(source.URL)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if !errors.Is(err, errCrossOriginRedirect) {
		t.Fatalf("Get() error = %v, want cross-origin rejection", err)
	}

	// Then
	select {
	case targetHeaders := <-observed:
		t.Fatalf("cross-origin target received headers: %#v", targetHeaders)
	default:
	}
}

func TestHTTPTransportPreservesCustomHeadersAcrossSameOriginRedirect(t *testing.T) {
	// Given
	observed := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/start" {
			http.Redirect(writer, request, "/final", http.StatusFound)
			return
		}
		observed <- request.Header.Clone()
	}))
	t.Cleanup(server.Close)
	transport := NewHTTPTransport(server.URL+"/start", sensitiveTestHeaders)

	// When
	response, err := transport.client.Get(server.URL + "/start")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	redirectedHeaders := <-observed

	// Then
	for name, values := range sensitiveTestHeaders {
		if got := redirectedHeaders.Values(name); len(got) != len(values) || got[0] != values[0] {
			t.Fatalf("same-origin header %s = %#v, want %#v", name, got, values)
		}
	}
}

func TestHTTPTransportDoesNotInjectCustomHeadersAcrossHTTPSDowngrade(t *testing.T) {
	// Given
	transport := NewHTTPTransport("https://mcp.example.test", sensitiveTestHeaders)
	roundTripper := transport.client.Transport.(*headerRoundTripper)
	var observed http.Header
	roundTripper.base = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		observed = request.Header.Clone()
		return emptyHTTPResponse(request), nil
	})
	request := httptest.NewRequest(http.MethodPost, "http://mcp.example.test", nil)

	// When
	response, err := roundTripper.RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	// Then
	assertNoCustomHeaders(t, observed)
}

func TestHTTPTransportMatchesCanonicalHostAndEffectivePort(t *testing.T) {
	// Given
	transport := NewHTTPTransport("https://MCP.EXAMPLE.TEST/path", sensitiveTestHeaders)
	roundTripper := transport.client.Transport.(*headerRoundTripper)
	var observed http.Header
	roundTripper.base = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		observed = request.Header.Clone()
		return emptyHTTPResponse(request), nil
	})
	request := httptest.NewRequest(http.MethodPost, "https://mcp.example.test:443/other", nil)

	// When
	response, err := roundTripper.RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	// Then
	if observed.Get("Authorization") != "Bearer secret" {
		t.Fatalf("canonical same-origin Authorization = %q, want configured value", observed.Get("Authorization"))
	}
}

func assertNoCustomHeaders(t *testing.T, headers http.Header) {
	t.Helper()
	for name := range sensitiveTestHeaders {
		if values := headers.Values(name); len(values) != 0 {
			t.Fatalf("cross-origin header %s leaked: %#v", name, values)
		}
	}
}
