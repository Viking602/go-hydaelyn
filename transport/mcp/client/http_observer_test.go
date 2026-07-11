package mcpclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type httpObservation struct {
	method              string
	rpcMethod           string
	accept              string
	requestContentType  string
	sessionID           string
	protocolVersion     string
	customHeader        string
	status              int
	responseContentType string
}

type observingRoundTripper struct {
	base http.RoundTripper
	mu   sync.Mutex
	seen []httpObservation
}

func (t *observingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	observation := httpObservation{
		method:             request.Method,
		rpcMethod:          requestRPCMethod(request),
		accept:             request.Header.Get("Accept"),
		requestContentType: request.Header.Get("Content-Type"),
		sessionID:          request.Header.Get("Mcp-Session-Id"),
		protocolVersion:    request.Header.Get("Mcp-Protocol-Version"),
		customHeader:       request.Header.Get("X-Hydaelyn-Test"),
	}
	response, err := t.base.RoundTrip(request)
	if response != nil {
		observation.status = response.StatusCode
		observation.responseContentType = response.Header.Get("Content-Type")
	}
	t.mu.Lock()
	t.seen = append(t.seen, observation)
	t.mu.Unlock()
	return response, err
}

func (t *observingRoundTripper) snapshot() []httpObservation {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]httpObservation(nil), t.seen...)
}

func (t *observingRoundTripper) findRPC(method string) (httpObservation, bool) {
	for _, observation := range t.snapshot() {
		if observation.rpcMethod == method {
			return observation, true
		}
	}
	return httpObservation{}, false
}

func (t *observingRoundTripper) findHTTP(method string) (httpObservation, bool) {
	for _, observation := range t.snapshot() {
		if observation.method == method {
			return observation, true
		}
	}
	return httpObservation{}, false
}

func (t *observingRoundTripper) requireRPC(testingT *testing.T, method string) httpObservation {
	testingT.Helper()
	observation, ok := t.findRPC(method)
	return requireObservation(testingT, observation, ok, method)
}

func (t *observingRoundTripper) requireHTTP(testingT *testing.T, method string) httpObservation {
	testingT.Helper()
	observation, ok := t.findHTTP(method)
	return requireObservation(testingT, observation, ok, method)
}

func requestRPCMethod(request *http.Request) string {
	if request.GetBody == nil {
		return ""
	}
	body, err := request.GetBody()
	if err != nil {
		return ""
	}
	defer func() { _ = body.Close() }()
	var message struct {
		Method string `json:"method"`
	}
	if err := json.NewDecoder(body).Decode(&message); err != nil {
		return ""
	}
	return message.Method
}

func newHTTPFeatureServer() *sdkmcp.Server {
	type echoArguments struct {
		Text string `json:"text"`
	}
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "http-server", Version: "v1.0.0"}, &sdkmcp.ServerOptions{
		GetSessionID: func() string { return "hydaelyn-session" },
	})
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "echo", Description: "Echo text"}, func(_ context.Context, _ *sdkmcp.CallToolRequest, arguments echoArguments) (*sdkmcp.CallToolResult, map[string]any, error) {
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: arguments.Text}},
		}, map[string]any{"text": arguments.Text}, nil
	})
	return server
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func emptyHTTPResponse(request *http.Request) *http.Response {
	return &http.Response{
		StatusCode: http.StatusNoContent,
		Header:     make(http.Header),
		Body:       io.NopCloser(http.NoBody),
		Request:    request,
	}
}

func requireObservation(t *testing.T, observation httpObservation, ok bool, name string) httpObservation {
	t.Helper()
	if !ok {
		t.Fatalf("missing HTTP observation for %s", name)
	}
	return observation
}
