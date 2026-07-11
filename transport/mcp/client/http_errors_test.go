package mcpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdkjsonrpc "github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHTTPTransportRejectsUnsupportedProtocolVersion(t *testing.T) {
	// Given
	httpServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer func() { _ = request.Body.Close() }()
		var message struct {
			ID any `json:"id"`
		}
		if err := json.NewDecoder(request.Body).Decode(&message); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      message.ID,
			"result": map[string]any{
				"protocolVersion": "1900-01-01",
				"capabilities":    map[string]any{},
				"serverInfo":      map[string]any{"name": "unsupported", "version": "v1.0.0"},
			},
		})
	}))
	t.Cleanup(httpServer.Close)
	client := New(NewHTTPTransport(httpServer.URL, nil))

	// When
	_, err := client.Initialize(context.Background(), "http-client", "v1.0.0")

	// Then
	if err == nil {
		t.Fatal("Initialize() error = nil, want unsupported protocol rejection")
	}
	if _, sessionErr := client.ListTools(context.Background()); !errors.Is(sessionErr, ErrNotInitialized) {
		t.Fatalf("ListTools() error = %v, want ErrNotInitialized", sessionErr)
	}
}

func TestHTTPTransportReturnsBadRequestFromInitialize(t *testing.T) {
	// Given
	httpServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "invalid MCP request", http.StatusBadRequest)
	}))
	t.Cleanup(httpServer.Close)
	client := New(NewHTTPTransport(httpServer.URL, nil))

	// When
	_, err := client.Initialize(context.Background(), "http-client", "v1.0.0")

	// Then
	if err == nil {
		t.Fatal("Initialize() error = nil, want HTTP 400 rejection")
	}
	if _, sessionErr := client.ListTools(context.Background()); !errors.Is(sessionErr, ErrNotInitialized) {
		t.Fatalf("ListTools() error = %v, want ErrNotInitialized", sessionErr)
	}
}

func TestHTTPTransportMapsExpiredSessionToSDKSentinel(t *testing.T) {
	// Given
	server := newHTTPFeatureServer()
	officialHandler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server {
		return server
	}, &sdkmcp.StreamableHTTPOptions{JSONResponse: true})
	httpServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		method, err := readAndRestoreRPCMethod(request)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		if method == "tools/list" {
			http.Error(writer, "session expired", http.StatusNotFound)
			return
		}
		officialHandler.ServeHTTP(writer, request)
	}))
	t.Cleanup(httpServer.Close)
	client := New(NewHTTPTransport(httpServer.URL, nil))
	t.Cleanup(func() { _ = client.Close() })
	if _, err := client.Initialize(context.Background(), "http-client", "v1.0.0"); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	// When
	_, err := client.ListTools(context.Background())

	// Then
	if !errors.Is(err, sdkmcp.ErrSessionMissing) {
		t.Fatalf("ListTools() error = %v, want ErrSessionMissing", err)
	}
}

func TestHTTPTransportPreservesTypedRPCError(t *testing.T) {
	// Given
	server := newHTTPFeatureServer()
	server.AddTool(&sdkmcp.Tool{Name: "fail", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return nil, &sdkjsonrpc.Error{Code: -32001, Message: "HTTP tool failed", Data: []byte(`{"detail":"boom"}`)}
	})
	httpServer := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server {
		return server
	}, &sdkmcp.StreamableHTTPOptions{JSONResponse: true}))
	t.Cleanup(httpServer.Close)
	client := New(NewHTTPTransport(httpServer.URL, nil))
	t.Cleanup(func() { _ = client.Close() })
	if _, err := client.Initialize(context.Background(), "http-client", "v1.0.0"); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	// When
	_, err := client.CallTool(context.Background(), "fail", nil)

	// Then
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != -32001 || string(rpcErr.Data) != `{"detail":"boom"}` {
		t.Fatalf("CallTool() error = %T %#v, want typed RPCError", err, rpcErr)
	}
	var sdkErr *sdkjsonrpc.Error
	if !errors.As(err, &sdkErr) {
		t.Fatalf("CallTool() error does not unwrap SDK error: %v", err)
	}
}

func TestHTTPTransportCallReturnsWhenContextCanceled(t *testing.T) {
	// Given
	started := make(chan struct{})
	finished := make(chan struct{})
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "cancel-server", Version: "v1.0.0"}, nil)
	server.AddTool(&sdkmcp.Tool{Name: "wait", InputSchema: map[string]any{"type": "object"}}, func(ctx context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		close(started)
		defer close(finished)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	httpServer := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server {
		return server
	}, &sdkmcp.StreamableHTTPOptions{JSONResponse: true}))
	t.Cleanup(httpServer.Close)
	client := New(NewHTTPTransport(httpServer.URL, nil))
	t.Cleanup(func() { _ = client.Close() })
	if _, err := client.Initialize(context.Background(), "http-client", "v1.0.0"); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	callContext, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := client.CallTool(callContext, "wait", nil)
		errCh <- err
	}()
	<-started

	// When
	cancel()

	// Then
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("CallTool() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CallTool() did not return after cancellation")
	}
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("server tool handler did not stop after cancellation")
	}
}

func readAndRestoreRPCMethod(request *http.Request) (string, error) {
	if request.Body == nil {
		return "", nil
	}
	payload, err := io.ReadAll(request.Body)
	if err != nil {
		return "", err
	}
	request.Body = io.NopCloser(bytes.NewReader(payload))
	var message struct {
		Method string `json:"method"`
	}
	if len(payload) == 0 {
		return "", nil
	}
	if err := json.Unmarshal(payload, &message); err != nil {
		return "", err
	}
	return message.Method, nil
}
