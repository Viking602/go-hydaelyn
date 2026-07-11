package mcpclient

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const defaultHTTPResponseHeaderTimeout = 30 * time.Second

var errCrossOriginRedirect = errors.New("mcp http transport: cross-origin redirect rejected")

type HTTPTransport struct {
	client     *http.Client
	delegate   *sdkmcp.StreamableClientTransport
	endpoint   string
	headers    http.Header
	limitState *inboundLimitState
	base       *http.Transport
	closeOnce  sync.Once
}

func NewHTTPTransport(endpoint string, headers http.Header) *HTTPTransport {
	cloned := make(http.Header)
	if headers != nil {
		cloned = headers.Clone()
	}
	base, privateBase := cloneDefaultHTTPTransport()
	limitState := newInboundLimitState()
	origin := parseHTTPOrigin(endpoint)
	client := &http.Client{
		Transport: &headerRoundTripper{
			base:           base,
			headers:        cloned,
			endpointOrigin: origin,
			limitState:     limitState,
		},
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			if !origin.matches(request.URL) {
				return errCrossOriginRedirect
			}
			return nil
		},
	}
	return &HTTPTransport{
		client:     client,
		endpoint:   endpoint,
		headers:    cloned,
		delegate:   &sdkmcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: client},
		limitState: limitState,
		base:       privateBase,
	}
}

func (t *HTTPTransport) Connect(ctx context.Context) (sdkmcp.Connection, error) {
	return t.delegate.Connect(ctx)
}

func (t *HTTPTransport) Close() error {
	t.closeOnce.Do(func() {
		if t.base != nil {
			t.base.CloseIdleConnections()
		}
	})
	return nil
}

func (t *HTTPTransport) inboundLimitError() error { return t.limitState.Err() }

type headerRoundTripper struct {
	base           http.RoundTripper
	headers        http.Header
	endpointOrigin httpOrigin
	limitState     *inboundLimitState
}

func (t *headerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method != http.MethodDelete {
		if err := t.limitState.Err(); err != nil {
			return nil, err
		}
	}
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	if t.endpointOrigin.matches(request.URL) {
		for name, values := range t.headers {
			if isSDKOwnedHTTPHeader(name) {
				continue
			}
			clone.Header.Del(name)
			for _, value := range values {
				clone.Header.Add(name, value)
			}
		}
	} else {
		stripOriginBoundHeaders(clone.Header)
	}
	response, err := t.base.RoundTrip(clone)
	if err != nil {
		return response, err
	}
	if request.Method == http.MethodDelete {
		if err := closeHTTPResponseBody(response); err != nil {
			return nil, err
		}
		return response, nil
	}
	limitHTTPResponse(response, t.limitState)
	return response, nil
}

func closeHTTPResponseBody(response *http.Response) error {
	if response == nil {
		return nil
	}
	body := response.Body
	response.Body = http.NoBody
	if body == nil {
		return nil
	}
	return body.Close()
}

func cloneDefaultHTTPTransport() (http.RoundTripper, *http.Transport) {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		clone := transport.Clone()
		clone.ResponseHeaderTimeout = defaultHTTPResponseHeaderTimeout
		return clone, clone
	}
	if http.DefaultTransport != nil {
		return http.DefaultTransport, nil
	}
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	transport.ResponseHeaderTimeout = defaultHTTPResponseHeaderTimeout
	return transport, transport
}

func stripOriginBoundHeaders(headers http.Header) {
	for _, name := range []string{
		"Authorization",
		"Cookie",
		"Proxy-Authorization",
		"Mcp-Session-Id",
		"Mcp-Protocol-Version",
		"Last-Event-Id",
	} {
		headers.Del(name)
	}
}

func isSDKOwnedHTTPHeader(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "Accept", "Content-Type", "Last-Event-Id", "Mcp-Protocol-Version", "Mcp-Session-Id":
		return true
	default:
		return false
	}
}
