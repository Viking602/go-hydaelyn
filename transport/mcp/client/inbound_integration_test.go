package mcpclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNewStreamTransportRejectsOversizeOfficialMessage(t *testing.T) {
	// Given
	serverConnection, clientConnection := net.Pipe()
	server := newOversizeFeatureServer()
	serverContext, cancelServer := context.WithCancel(context.Background())
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run(serverContext, &sdkmcp.IOTransport{Reader: serverConnection, Writer: serverConnection})
	}()
	client := New(NewStreamTransport(clientConnection, clientConnection))
	if _, err := client.Initialize(context.Background(), "stream-client", "v1.0.0"); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		cancelServer()
		<-serverDone
	})

	// When
	_, err := client.CallTool(context.Background(), "oversize", nil)

	// Then
	assertDefaultInboundLimitError(t, err)
}

func TestNewStreamTransportRejectsMultilineJSONFrame(t *testing.T) {
	// Given
	serverConnection, clientConnection := net.Pipe()
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		defer serverConnection.Close()
		requestLine, _ := bufio.NewReader(serverConnection).ReadBytes('\n')
		var request struct {
			ID any `json:"id"`
		}
		_ = json.Unmarshal(requestLine, &request)
		response, _ := json.MarshalIndent(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"result": map[string]any{
				"protocolVersion": "2025-06-18",
				"capabilities":    map[string]any{},
				"serverInfo":      map[string]any{"name": "pretty", "version": "v1.0.0"},
			},
		}, "", "  ")
		_, _ = serverConnection.Write(append(response, '\n'))
		_, _ = io.Copy(io.Discard, serverConnection)
	}()
	client := New(NewStreamTransport(clientConnection, clientConnection))

	// When
	_, err := client.Initialize(context.Background(), "stream-client", "v1.0.0")
	closeErr := client.Close()
	<-serverDone

	// Then
	assertInvalidFrameError(t, err)
	if closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}
}

func TestHTTPTransportRejectsOversizeJSONAndFailsFast(t *testing.T) {
	// Given
	client, transport, requests, deletes := newOversizeHTTPClient(t, true)

	// When
	_, err := client.CallTool(context.Background(), "oversize", nil)
	requestsAfterOverflow := requests.Load()
	_, repeatedErr := client.ListTools(context.Background())
	requestsAfterFailFast := requests.Load()
	closeErr := client.Close()

	// Then
	assertDefaultInboundLimitError(t, err)
	assertDefaultInboundLimitError(t, repeatedErr)
	if requestsAfterFailFast != requestsAfterOverflow {
		t.Fatalf("fail-fast request count = %d, want %d", requestsAfterFailFast, requestsAfterOverflow)
	}
	if closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}
	if deletes.Load() != 1 {
		t.Fatalf("DELETE requests = %d, want 1", deletes.Load())
	}
	if transport.inboundLimitError() == nil {
		t.Fatal("HTTP transport did not retain sticky inbound limit error")
	}
}

func TestHTTPTransportRejectsOversizeSSEEventWithoutUnboundedReconnect(t *testing.T) {
	// Given
	client, _, requests, _ := newOversizeHTTPClient(t, false)
	callContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	requestsBeforeCall := requests.Load()

	// When
	_, err := client.CallTool(callContext, "oversize", nil)

	// Then
	assertDefaultInboundLimitError(t, err)
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("SSE overflow retried until the caller deadline")
	}
	if attempts := requests.Load() - requestsBeforeCall; attempts > 2 {
		t.Fatalf("SSE overflow network attempts = %d, want at most 2", attempts)
	}
}

func TestHTTPTransportAllowsMultipleSSEEventsAboveAggregateLimit(t *testing.T) {
	// Given
	eventData := strings.Repeat("x", 1<<20)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		for range 5 {
			_, _ = io.WriteString(writer, "data:"+eventData+"\n\n")
		}
	}))
	t.Cleanup(server.Close)
	transport := NewHTTPTransport(server.URL, nil)

	// When
	response, err := transport.client.Get(server.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	payload, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()

	// Then
	if readErr != nil {
		t.Fatalf("ReadAll() error = %v", readErr)
	}
	if len(payload) <= maxInboundMessageBytes {
		t.Fatalf("aggregate SSE payload = %d, want above %d", len(payload), maxInboundMessageBytes)
	}
}

func newOversizeHTTPClient(t *testing.T, jsonResponse bool) (*Client, *HTTPTransport, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	server := newOversizeFeatureServer()
	officialHandler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server {
		return server
	}, &sdkmcp.StreamableHTTPOptions{JSONResponse: jsonResponse})
	var requests atomic.Int32
	var deletes atomic.Int32
	httpServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.Method == http.MethodDelete {
			deletes.Add(1)
		}
		officialHandler.ServeHTTP(writer, request)
	}))
	t.Cleanup(httpServer.Close)
	transport := NewHTTPTransport(httpServer.URL, nil)
	client := New(transport)
	if _, err := client.Initialize(context.Background(), "http-client", "v1.0.0"); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, transport, &requests, &deletes
}

func newOversizeFeatureServer() *sdkmcp.Server {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "oversize-server", Version: "v1.0.0"}, nil)
	oversize := strings.Repeat("x", maxInboundMessageBytes+1)
	server.AddTool(&sdkmcp.Tool{Name: "oversize", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: oversize}}}, nil
	})
	return server
}

func assertDefaultInboundLimitError(t *testing.T, err error) {
	t.Helper()
	var limitErr *InboundLimitError
	if !errors.As(err, &limitErr) || limitErr.Limit != maxInboundMessageBytes {
		t.Fatalf("error = %T %v, want InboundLimitError(%d)", err, err, maxInboundMessageBytes)
	}
}

func assertInvalidFrameError(t *testing.T, err error) {
	t.Helper()
	var frameErr *InvalidFrameError
	if !errors.Is(err, ErrInvalidFrame) || !errors.As(err, &frameErr) {
		t.Fatalf("error = %T %v, want InvalidFrameError", err, err)
	}
}
