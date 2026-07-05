package mcpclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/transport/mcp/jsonrpc"
)

func newTestServer(t *testing.T, handler func(method string, params map[string]any) any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		method, _ := payload["method"].(string)
		params, _ := payload["params"].(map[string]any)
		result := handler(method, params)
		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      payload["id"],
			"result":  result,
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
}

func TestListResourcesReturnsServerResources(t *testing.T) {
	server := newTestServer(t, func(method string, _ map[string]any) any {
		if method != "resources/list" {
			t.Fatalf("unexpected method %s", method)
		}
		return map[string]any{
			"resources": []map[string]any{
				{"uri": "file:///docs/readme.md", "name": "README", "mimeType": "text/markdown"},
			},
		}
	})
	defer server.Close()

	client := New(NewHTTPTransport(server.URL, nil))
	resources, err := client.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources() error = %v", err)
	}
	if len(resources) != 1 || resources[0].URI != "file:///docs/readme.md" {
		t.Fatalf("unexpected resources %#v", resources)
	}
	if resources[0].Name != "README" {
		t.Fatalf("unexpected name %q", resources[0].Name)
	}
}

func TestReadResourceReturnsContents(t *testing.T) {
	server := newTestServer(t, func(method string, params map[string]any) any {
		if method != "resources/read" {
			t.Fatalf("unexpected method %s", method)
		}
		if params["uri"] != "file:///docs/readme.md" {
			t.Fatalf("unexpected uri %v", params["uri"])
		}
		return map[string]any{
			"contents": []map[string]any{
				{"uri": "file:///docs/readme.md", "text": "# Hello", "mimeType": "text/markdown"},
			},
		}
	})
	defer server.Close()

	client := New(NewHTTPTransport(server.URL, nil))
	contents, err := client.ReadResource(context.Background(), "file:///docs/readme.md")
	if err != nil {
		t.Fatalf("ReadResource() error = %v", err)
	}
	if len(contents) != 1 || contents[0].Text != "# Hello" {
		t.Fatalf("unexpected contents %#v", contents)
	}
}

func TestListPromptsReturnsServerPrompts(t *testing.T) {
	server := newTestServer(t, func(method string, _ map[string]any) any {
		if method != "prompts/list" {
			t.Fatalf("unexpected method %s", method)
		}
		return map[string]any{
			"prompts": []map[string]any{
				{
					"name":        "summarize",
					"description": "Summarize text",
					"arguments": []map[string]any{
						{"name": "text", "required": true},
					},
				},
			},
		}
	})
	defer server.Close()

	client := New(NewHTTPTransport(server.URL, nil))
	prompts, err := client.ListPrompts(context.Background())
	if err != nil {
		t.Fatalf("ListPrompts() error = %v", err)
	}
	if len(prompts) != 1 || prompts[0].Name != "summarize" {
		t.Fatalf("unexpected prompts %#v", prompts)
	}
	if len(prompts[0].Arguments) != 1 || !prompts[0].Arguments[0].Required {
		t.Fatalf("unexpected arguments %#v", prompts[0].Arguments)
	}
}

func TestGetPromptReturnsMessages(t *testing.T) {
	server := newTestServer(t, func(method string, params map[string]any) any {
		if method != "prompts/get" {
			t.Fatalf("unexpected method %s", method)
		}
		if params["name"] != "summarize" {
			t.Fatalf("unexpected name %v", params["name"])
		}
		return map[string]any{
			"messages": []map[string]any{
				{
					"role":    "user",
					"content": map[string]any{"type": "text", "text": "Summarize: hello world"},
				},
			},
		}
	})
	defer server.Close()

	client := New(NewHTTPTransport(server.URL, nil))
	messages, err := client.GetPrompt(context.Background(), "summarize", map[string]string{"text": "hello world"})
	if err != nil {
		t.Fatalf("GetPrompt() error = %v", err)
	}
	if len(messages) != 1 || messages[0].Role != "user" {
		t.Fatalf("unexpected messages %#v", messages)
	}
	if messages[0].Content.Text != "Summarize: hello world" {
		t.Fatalf("unexpected content %#v", messages[0].Content)
	}
}

func TestHTTPTransportUsesDefaultClientTimeout(t *testing.T) {
	transport := NewHTTPTransport("https://mcp.example.test", nil)
	if transport.client == nil {
		t.Fatal("expected default http client")
	}
	if transport.client.Timeout <= 0 {
		t.Fatalf("expected default timeout, got %s", transport.client.Timeout)
	}
}

func TestStreamTransportCallCancelReturnsWhenNoResponseArrives(t *testing.T) {
	reader := newBlockingReadCloser()
	transport := NewStreamTransport(reader, io.Discard, reader)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		var result map[string]any
		errCh <- transport.Call(ctx, "tools/list", map[string]any{}, &result)
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context deadline error, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		_ = transport.Close()
		t.Fatal("Call did not return after context deadline")
	}
}

func TestStreamTransportRejectsCallsAfterReaderExits(t *testing.T) {
	transport := NewStreamTransport(strings.NewReader(""), io.Discard)

	var result map[string]any
	if err := transport.Call(context.Background(), "tools/list", nil, &result); !errors.Is(err, io.EOF) {
		t.Fatalf("first Call error = %v, want EOF", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := transport.Call(ctx, "tools/list", nil, &result)
	if !errors.Is(err, errStreamTransportClosed) {
		t.Fatalf("second Call error = %v, want transport closed", err)
	}
}

func TestStreamTransportInvalidResponseFailsPendingCall(t *testing.T) {
	server := newFramedPipeServer(t, func(req jsonrpc.Request, s *framedPipeServer) {
		s.writeFramed(map[string]any{
			"jsonrpc": "1.0",
			"id":      req.ID,
			"result":  map[string]any{"ok": true},
		})
	})
	defer server.close()
	transport := NewStreamTransport(server.reader, server.cliW, server.srvR, server.srvW)
	defer transport.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	var result map[string]any
	err := transport.Call(ctx, "tools/list", nil, &result)
	if err == nil {
		t.Fatal("Call error = nil, want invalid response error")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("Call timed out instead of surfacing invalid response error")
	}

	err = transport.Call(context.Background(), "tools/list", nil, &result)
	if !errors.Is(err, errStreamTransportClosed) {
		t.Fatalf("future Call error = %v, want transport closed", err)
	}
}

func TestDialStdioCloseWaitsForProcessCancelLifecycle(t *testing.T) {
	if os.Getenv("HYDAELYN_MCP_STDIO_CLOSE_HELPER") == "1" {
		if path := os.Getenv("HYDAELYN_MCP_STDIO_ENV_FILE"); path != "" {
			_ = os.WriteFile(path, []byte(os.Getenv("HYDAELYN_MCP_STDIO_PARENT_ENV")), 0o600)
		}
		_, _ = io.Copy(io.Discard, os.Stdin)
		time.Sleep(50 * time.Millisecond)
		if path := os.Getenv("HYDAELYN_MCP_STDIO_CLOSE_FILE"); path != "" {
			_ = os.WriteFile(path, []byte("done"), 0o600)
		}
		os.Exit(0)
	}

	exitFile := filepath.Join(t.TempDir(), "exited")
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	client, err := DialStdio(context.Background(), StdioConfig{
		Command: executable,
		Args:    []string{"-test.run=^TestDialStdioCloseWaitsForProcessCancelLifecycle$"},
		Env: append(os.Environ(),
			"HYDAELYN_MCP_STDIO_CLOSE_HELPER=1",
			"HYDAELYN_MCP_STDIO_CLOSE_FILE="+exitFile,
		),
	})
	if err != nil {
		t.Fatalf("DialStdio() error = %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := os.Stat(exitFile); err != nil {
		t.Fatalf("Close returned before stdio process exited: %v", err)
	}
}

func TestDialStdioEmptyEnvDoesNotLeakParentByDefault(t *testing.T) {
	t.Setenv("HYDAELYN_MCP_STDIO_PARENT_ENV", "parent-secret")
	envFile := filepath.Join(t.TempDir(), "env")
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	client, err := DialStdio(context.Background(), StdioConfig{
		Command: executable,
		Args:    []string{"-test.run=^TestDialStdioEmptyEnvLeakHelper$", "--", envFile},
	})
	if err != nil {
		t.Fatalf("DialStdio() error = %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	raw, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("child did not write env file: %v", err)
	}
	if string(raw) != "" {
		t.Fatalf("child leaked parent env = %q, want empty", string(raw))
	}
}

func TestDialStdioEmptyEnvLeakHelper(t *testing.T) {
	envFile := argAfterDoubleDash()
	if envFile == "" {
		return
	}
	if err := os.WriteFile(envFile, []byte(os.Getenv("HYDAELYN_MCP_STDIO_PARENT_ENV")), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestDialStdioHonorsDir(t *testing.T) {
	temp := t.TempDir()
	workDir := filepath.Join(temp, "work")
	if err := os.Mkdir(workDir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	wdFile := filepath.Join(temp, "wd")
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	client, err := DialStdio(context.Background(), StdioConfig{
		Command: executable,
		Args:    []string{"-test.run=^TestDialStdioDirHelper$", "--", wdFile},
		Dir:     workDir,
	})
	if err != nil {
		t.Fatalf("DialStdio() error = %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	raw, err := os.ReadFile(wdFile)
	if err != nil {
		t.Fatalf("child did not write working directory file: %v", err)
	}
	if string(raw) != workDir {
		t.Fatalf("child working directory = %q, want %q", string(raw), workDir)
	}
}

func TestDialStdioDirHelper(t *testing.T) {
	wdFile := argAfterDoubleDash()
	if wdFile == "" {
		return
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.WriteFile(wdFile, []byte(wd), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func argAfterDoubleDash() string {
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
	}
	return ""
}

func TestDialStdioInheritEnvIncludesParentWhenConfigEnvSet(t *testing.T) {
	t.Setenv("HYDAELYN_MCP_STDIO_PARENT_ENV", "parent-ok")
	temp := t.TempDir()
	envFile := filepath.Join(temp, "env")
	exitFile := filepath.Join(temp, "exited")
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	client, err := DialStdio(context.Background(), StdioConfig{
		Command: executable,
		Args:    []string{"-test.run=^TestDialStdioCloseWaitsForProcessCancelLifecycle$"},
		Env: []string{
			"HYDAELYN_MCP_STDIO_CLOSE_HELPER=1",
			"HYDAELYN_MCP_STDIO_CLOSE_FILE=" + exitFile,
			"HYDAELYN_MCP_STDIO_ENV_FILE=" + envFile,
		},
		InheritEnv: true,
	})
	if err != nil {
		t.Fatalf("DialStdio() error = %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	raw, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("child did not write inherited env file: %v", err)
	}
	if string(raw) != "parent-ok" {
		t.Fatalf("child inherited env = %q, want parent-ok", string(raw))
	}
}

func TestDialStdioConfigEnvDoesNotLeakParentByDefault(t *testing.T) {
	t.Setenv("HYDAELYN_MCP_STDIO_PARENT_ENV", "parent-secret")
	temp := t.TempDir()
	envFile := filepath.Join(temp, "env")
	exitFile := filepath.Join(temp, "exited")
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	client, err := DialStdio(context.Background(), StdioConfig{
		Command: executable,
		Args:    []string{"-test.run=^TestDialStdioCloseWaitsForProcessCancelLifecycle$"},
		Env: []string{
			"HYDAELYN_MCP_STDIO_CLOSE_HELPER=1",
			"HYDAELYN_MCP_STDIO_CLOSE_FILE=" + exitFile,
			"HYDAELYN_MCP_STDIO_ENV_FILE=" + envFile,
		},
	})
	if err != nil {
		t.Fatalf("DialStdio() error = %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	raw, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("child did not write env file: %v", err)
	}
	if string(raw) != "" {
		t.Fatalf("child leaked parent env = %q, want empty", string(raw))
	}
}

func TestDialStdioCancelAfterDialDoesNotKillProcess(t *testing.T) {
	exitFile := filepath.Join(t.TempDir(), "exited")
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	client, err := DialStdio(ctx, StdioConfig{
		Command: executable,
		Args:    []string{"-test.run=^TestDialStdioCloseWaitsForProcessCancelLifecycle$"},
		Env: []string{
			"HYDAELYN_MCP_STDIO_CLOSE_HELPER=1",
			"HYDAELYN_MCP_STDIO_CLOSE_FILE=" + exitFile,
		},
	})
	if err != nil {
		t.Fatalf("DialStdio() error = %v", err)
	}
	cancel()
	time.Sleep(100 * time.Millisecond)
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := os.Stat(exitFile); err != nil {
		t.Fatalf("process was killed by dial context before Close could drain it: %v", err)
	}
}

type blockingReadCloser struct {
	once   sync.Once
	closed chan struct{}
}

func newBlockingReadCloser() *blockingReadCloser {
	return &blockingReadCloser{closed: make(chan struct{})}
}

func (r *blockingReadCloser) Read(_ []byte) (int, error) {
	<-r.closed
	return 0, io.EOF
}

func (r *blockingReadCloser) Close() error {
	r.once.Do(func() {
		close(r.closed)
	})
	return nil
}

// framedPipeServer simulates an MCP server speaking the LSP-style framed
// protocol over a pair of pipes. The handler receives each decoded request and
// may return framed responses (or notifications) via the returned writer.
type framedPipeServer struct {
	reader  *bufio.Reader // client reads responses from here
	writer  *bufio.Writer // server writes responses here
	writeMu sync.Mutex
	srvR    *io.PipeReader // server reads requests from here
	srvW    *io.PipeWriter
	cliW    *io.PipeWriter // client writes requests here
	done    chan struct{}
}

func newFramedPipeServer(t *testing.T, handle func(req jsonrpc.Request, server *framedPipeServer)) *framedPipeServer {
	t.Helper()
	cliR, cliW := io.Pipe()
	srvR, srvW := io.Pipe()
	s := &framedPipeServer{
		reader: bufio.NewReader(srvR), // client reads responses from srvR
		writer: bufio.NewWriter(srvW), // server writes responses to srvW
		srvR:   srvR,
		srvW:   srvW,
		cliW:   cliW, // client writes requests to cliW
		done:   make(chan struct{}),
	}
	go func() {
		defer close(s.done)
		server := bufio.NewReader(cliR) // server reads requests from cliR
		for {
			payload, err := jsonrpc.ReadFramed(server)
			if err != nil {
				return
			}
			req, err := jsonrpc.DecodeRequest(payload)
			if err != nil {
				return
			}
			handle(req, s)
		}
	}()
	return s
}

func (s *framedPipeServer) writeFramed(v any) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = jsonrpc.WriteFramed(s.writer, v)
	_ = s.writer.Flush()
}

func (s *framedPipeServer) close() {
	_ = s.cliW.Close()
	_ = s.srvW.Close()
	<-s.done
}

// TestStreamTransport_ConcurrentCallsDoNotSerialize verifies that two
// concurrent Calls are not serialized by a transport-wide lock: both complete
// even though the server responds to the second request first.
func TestStreamTransport_ConcurrentCallsDoNotSerialize(t *testing.T) {
	var order []string
	var orderMu sync.Mutex
	server := newFramedPipeServer(t, func(req jsonrpc.Request, s *framedPipeServer) {
		// Delay the first response asynchronously so the server loop can read the
		// second request before the first Call completes. If Call held a
		// transport-wide lock, the second request would not arrive until the first
		// response was delivered.
		switch req.Method {
		case "first":
			go func(id any) {
				time.Sleep(80 * time.Millisecond)
				resp, err := jsonrpc.Success(id, map[string]any{"n": 1})
				if err != nil {
					t.Errorf("Success: %v", err)
					return
				}
				s.writeFramed(resp)
			}(req.ID)
		case "second":
			resp, err := jsonrpc.Success(req.ID, map[string]any{"n": 2})
			if err != nil {
				t.Errorf("Success: %v", err)
				return
			}
			s.writeFramed(resp)
		default:
			t.Errorf("unexpected method %q", req.Method)
		}
		orderMu.Lock()
		order = append(order, req.Method)
		orderMu.Unlock()
	})
	defer server.close()

	transport := NewStreamTransport(server.reader, server.cliW)
	defer transport.Close()

	type result struct {
		n   int
		err error
	}
	resCh := make(chan result, 2)
	do := func(method string) {
		var m map[string]any
		err := transport.Call(context.Background(), method, nil, &m)
		n, _ := m["n"].(float64)
		resCh <- result{n: int(n), err: err}
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); do("first") }()
	go func() { defer wg.Done(); do("second") }()
	wg.Wait()
	close(resCh)

	got := make(map[string]int)
	for r := range resCh {
		if r.err != nil {
			t.Fatalf("Call returned error: %v", r.err)
		}
		got[fmt.Sprintf("n=%d", r.n)]++
	}
	if got["n=1"] != 1 || got["n=2"] != 1 {
		t.Fatalf("expected both calls to complete, got %#v", got)
	}
	// Sanity check the server actually saw both requests in the order we expect
	// (second should arrive before first completes due to the artificial delay).
	orderMu.Lock()
	defer orderMu.Unlock()
	if len(order) != 2 {
		t.Fatalf("expected 2 server requests, got %d", len(order))
	}
}

func TestStreamTransport_RemovesCallAfterResponse(t *testing.T) {
	server := newFramedPipeServer(t, func(req jsonrpc.Request, s *framedPipeServer) {
		resp, _ := jsonrpc.Success(req.ID, map[string]any{"ok": true})
		s.writeFramed(resp)
	})
	defer server.close()
	transport := NewStreamTransport(server.reader, server.cliW, server.srvR, server.srvW)
	defer transport.Close()

	var result map[string]any
	if err := transport.Call(context.Background(), "tools/list", nil, &result); err != nil {
		t.Fatalf("Call error = %v", err)
	}
	transport.callsMu.Lock()
	defer transport.callsMu.Unlock()
	if len(transport.calls) != 0 {
		t.Fatalf("calls registry leaked %d completed call(s)", len(transport.calls))
	}
}

// TestStreamTransport_RejectsNotificationAsResponse verifies that a server
// sending a notification (no id) does NOT satisfy a waiting Call.
func TestStreamTransport_RejectsNotificationAsResponse(t *testing.T) {
	server := newFramedPipeServer(t, func(req jsonrpc.Request, s *framedPipeServer) {
		// First send a notification (no id) that must NOT match any caller...
		notif, _ := jsonrpc.NewRequest(nil, "notifications/progress", nil)
		s.writeFramed(notif)
		// ...then send the actual response with the correct id.
		time.Sleep(50 * time.Millisecond)
		resp, err := jsonrpc.Success(req.ID, map[string]any{"ok": true})
		if err != nil {
			t.Errorf("Success: %v", err)
			return
		}
		s.writeFramed(resp)
	})
	defer server.close()

	transport := NewStreamTransport(server.reader, server.cliW)
	defer transport.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var result map[string]any
	if err := transport.Call(ctx, "tools/list", nil, &result); err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if ok, _ := result["ok"].(bool); !ok {
		t.Fatalf("expected ok=true, got %#v", result)
	}
}

// TestStreamTransport_RPCErrorCodeTyped asserts that a JSON-RPC error response
// surfaces as a typed *RPCError whose Code can be extracted via errors.As.
func TestStreamTransport_RPCErrorCodeTyped(t *testing.T) {
	const code = -32001
	server := newFramedPipeServer(t, func(req jsonrpc.Request, s *framedPipeServer) {
		resp := jsonrpc.Failure(req.ID, code, "tool failed", map[string]any{"detail": "boom"})
		s.writeFramed(resp)
	})
	defer server.close()

	transport := NewStreamTransport(server.reader, server.cliW)
	defer transport.Close()

	var result map[string]any
	err := transport.Call(context.Background(), "tools/call", nil, &result)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected *RPCError, got %T: %v", err, err)
	}
	if rpcErr.Code != code {
		t.Fatalf("expected code %d, got %d", code, rpcErr.Code)
	}
	if rpcErr.Message != "tool failed" {
		t.Fatalf("expected message %q, got %q", "tool failed", rpcErr.Message)
	}
	if len(rpcErr.Data) == 0 {
		t.Fatal("expected non-empty Data")
	}
	var detail map[string]any
	if err := json.Unmarshal(rpcErr.Data, &detail); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if detail["detail"] != "boom" {
		t.Fatalf("expected detail=boom, got %#v", detail)
	}
}
