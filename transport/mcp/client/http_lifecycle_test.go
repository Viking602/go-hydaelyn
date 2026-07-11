package mcpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHTTPTransportCompletesOfficialStreamableLifecycle(t *testing.T) {
	tests := []struct {
		name                string
		jsonResponse        bool
		wantResponseContent string
	}{
		{"JSON responses", true, "application/json"},
		{"SSE responses", false, "text/event-stream"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			server := newHTTPFeatureServer()
			httpServer := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server {
				return server
			}, &sdkmcp.StreamableHTTPOptions{JSONResponse: test.jsonResponse}))
			t.Cleanup(httpServer.Close)
			transport := NewHTTPTransport(httpServer.URL, http.Header{"X-Hydaelyn-Test": {"preserved"}})
			headerTransport := transport.client.Transport.(*headerRoundTripper)
			observer := &observingRoundTripper{base: headerTransport.base}
			headerTransport.base = observer
			client := New(transport)
			t.Cleanup(func() { _ = client.Close() })

			// When
			initialized, err := client.Initialize(context.Background(), "http-client", "v1.0.0")
			if err != nil {
				t.Fatalf("Initialize() error = %v", err)
			}
			tools, err := client.ListTools(context.Background())
			if err != nil {
				t.Fatalf("ListTools() error = %v", err)
			}
			result, err := client.CallTool(context.Background(), "echo", map[string]any{"text": "hello"})
			if err != nil {
				t.Fatalf("CallTool() error = %v", err)
			}
			if err := client.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}

			// Then
			if initialized.ServerInfo.Name != "http-server" || initialized.ProtocolVersion == "" {
				t.Fatalf("Initialize() = %#v", initialized)
			}
			if len(tools) != 1 || tools[0].Name != "echo" || tools[0].InputSchema.Type != "object" {
				t.Fatalf("ListTools() = %#v", tools)
			}
			if len(result.Content) != 1 || result.Content[0].Text != "hello" || result.StructuredContent["text"] != "hello" {
				t.Fatalf("CallTool() = %#v", result)
			}
			initialize := observer.requireRPC(t, "initialize")
			assertSDKRequestHeaders(t, initialize, false)
			initializedRequest := observer.requireRPC(t, "notifications/initialized")
			assertSDKRequestHeaders(t, initializedRequest, true)
			if initializedRequest.status != http.StatusAccepted {
				t.Fatalf("initialized status = %d, want 202", initializedRequest.status)
			}
			call := observer.requireRPC(t, "tools/call")
			assertSDKRequestHeaders(t, call, true)
			if !strings.HasPrefix(call.responseContentType, test.wantResponseContent) {
				t.Fatalf("tools/call response Content-Type = %q, want %q", call.responseContentType, test.wantResponseContent)
			}
			deleted := observer.requireHTTP(t, http.MethodDelete)
			if deleted.sessionID != "hydaelyn-session" || deleted.protocolVersion != initialized.ProtocolVersion {
				t.Fatalf("DELETE headers = session %q protocol %q", deleted.sessionID, deleted.protocolVersion)
			}
		})
	}
}

func TestHTTPTransportClonesCustomAndRequestHeaders(t *testing.T) {
	// Given
	headers := http.Header{"X-Hydaelyn-Test": {"original"}, "Accept": {"text/plain"}}
	transport := NewHTTPTransport("https://mcp.example.test", headers)
	headers.Set("X-Hydaelyn-Test", "mutated")
	roundTripper := transport.client.Transport.(*headerRoundTripper)
	var observed http.Header
	roundTripper.base = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		observed = request.Header.Clone()
		return emptyHTTPResponse(request), nil
	})
	request := httptest.NewRequest(http.MethodPost, transport.endpoint, nil)
	request.Header.Set("Accept", "application/json, text/event-stream")

	// When
	response, err := roundTripper.RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	// Then
	if observed.Get("X-Hydaelyn-Test") != "original" {
		t.Fatalf("custom header = %q, want original", observed.Get("X-Hydaelyn-Test"))
	}
	if observed.Get("Accept") != "application/json, text/event-stream" {
		t.Fatalf("Accept = %q, want SDK value", observed.Get("Accept"))
	}
	if request.Header.Get("X-Hydaelyn-Test") != "" {
		t.Fatalf("shared request was mutated: %#v", request.Header)
	}
}

func TestHTTPTransportUsesThirtySecondResponseHeaderTimeout(t *testing.T) {
	// When
	transport := NewHTTPTransport("https://mcp.example.test", nil)

	// Then
	if transport.client.Timeout != 0 {
		t.Fatalf("HTTP total timeout = %s, want no body lifetime limit", transport.client.Timeout)
	}
	roundTripper := transport.client.Transport.(*headerRoundTripper)
	base, ok := roundTripper.base.(*http.Transport)
	if !ok {
		t.Fatalf("HTTP base transport = %T, want *http.Transport", roundTripper.base)
	}
	if base == http.DefaultTransport {
		t.Fatal("HTTP base transport reuses mutable http.DefaultTransport")
	}
	if base.ResponseHeaderTimeout != defaultHTTPResponseHeaderTimeout {
		t.Fatalf("response header timeout = %s, want %s", base.ResponseHeaderTimeout, defaultHTTPResponseHeaderTimeout)
	}
}

func TestNewHTTPTransportDoesNotPanicWhenDefaultTransportIsReplaced(t *testing.T) {
	// Given
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	called := false
	http.DefaultTransport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		called = true
		return emptyHTTPResponse(request), nil
	})

	// When
	transport := NewHTTPTransport("https://mcp.example.test", nil)

	// Then
	if transport.client.Transport == nil {
		t.Fatal("NewHTTPTransport() returned a nil RoundTripper")
	}
	response, err := transport.client.Get("https://mcp.example.test")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	_ = response.Body.Close()
	if !called {
		t.Fatal("custom http.DefaultTransport was not called")
	}
}

func assertSDKRequestHeaders(t *testing.T, observation httpObservation, initialized bool) {
	t.Helper()
	if observation.accept != "application/json, text/event-stream" || observation.requestContentType != "application/json" {
		t.Fatalf("SDK media headers = Accept %q Content-Type %q", observation.accept, observation.requestContentType)
	}
	if observation.customHeader != "preserved" {
		t.Fatalf("custom header = %q, want preserved", observation.customHeader)
	}
	if initialized && (observation.sessionID != "hydaelyn-session" || observation.protocolVersion == "") {
		t.Fatalf("SDK session headers = session %q protocol %q", observation.sessionID, observation.protocolVersion)
	}
}
